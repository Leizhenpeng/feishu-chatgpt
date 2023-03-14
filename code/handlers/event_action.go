package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larksheets "github.com/larksuite/oapi-sdk-go/v3/service/sheets/v3"
	"github.com/pkg/errors"
	"net/url"
	"os"
	"start-feishubot/initialization"
	"start-feishubot/services"
	larksheetsV2 "start-feishubot/services/larksheets/v2"
	"start-feishubot/services/openai"
	"start-feishubot/utils"
	"start-feishubot/utils/audio"
	"strings"
)

type MsgInfo struct {
	handlerType HandlerType
	msgType     string
	msgId       *string
	chatId      *string
	qParsed     string
	fileKey     string
	imageKey    string
	sessionId   *string
	mention     []*larkim.MentionEvent
}
type ActionInfo struct {
	handler *MessageHandler
	ctx     *context.Context
	info    *MsgInfo
}

type Action interface {
	Execute(a *ActionInfo) bool
}

type ProcessedUniqueAction struct { //消息唯一性
}

func (*ProcessedUniqueAction) Execute(a *ActionInfo) bool {
	if a.handler.msgCache.IfProcessed(*a.info.msgId) {
		return false
	}
	a.handler.msgCache.TagProcessed(*a.info.msgId)
	return true
}

type ProcessMentionAction struct { //是否机器人应该处理
}

func (*ProcessMentionAction) Execute(a *ActionInfo) bool {
	// 私聊直接过
	if a.info.handlerType == UserHandler {
		return true
	}
	// 群聊判断是否提到机器人
	if a.info.handlerType == GroupHandler {
		if a.handler.judgeIfMentionMe(a.info.mention) {
			return true
		}
		return false
	}
	return false
}

type EmptyAction struct { /*空消息*/
}

func (*EmptyAction) Execute(a *ActionInfo) bool {
	if len(a.info.qParsed) == 0 {
		sendMsg(*a.ctx, "🤖️：你想知道什么呢~", a.info.chatId)
		fmt.Println("msgId", *a.info.msgId,
			"message.text is empty")
		return false
	}
	return true
}

type ClearAction struct { /*清除消息*/
}

func (*ClearAction) Execute(a *ActionInfo) bool {
	if _, foundClear := utils.EitherTrimEqual(a.info.qParsed,
		"/clear", "清除"); foundClear {
		sendClearCacheCheckCard(*a.ctx, a.info.sessionId,
			a.info.msgId)
		return false
	}
	return true
}

type RolePlayAction struct { /*角色扮演*/
}

func (*RolePlayAction) Execute(a *ActionInfo) bool {
	if system, foundSystem := utils.EitherCutPrefix(a.info.qParsed,
		"/system ", "角色扮演 "); foundSystem {
		a.handler.sessionCache.Clear(*a.info.sessionId)
		systemMsg := append([]openai.Messages{}, openai.Messages{
			Role: "system", Content: system,
		})
		a.handler.sessionCache.SetMsg(*a.info.sessionId, systemMsg)
		sendSystemInstructionCard(*a.ctx, a.info.sessionId,
			a.info.msgId, system)
		return false
	}
	return true
}

type HelpAction struct { /*帮助*/
}

func (*HelpAction) Execute(a *ActionInfo) bool {
	if _, foundHelp := utils.EitherTrimEqual(a.info.qParsed, "/help",
		"帮助"); foundHelp {
		sendHelpCard(*a.ctx, a.info.sessionId, a.info.msgId)
		return false
	}
	return true
}

type PicAction struct { /*图片*/
}

func (*PicAction) Execute(a *ActionInfo) bool {
	// 开启图片创作模式
	if _, foundPic := utils.EitherTrimEqual(a.info.qParsed,
		"/picture", "图片创作"); foundPic {
		a.handler.sessionCache.Clear(*a.info.sessionId)
		a.handler.sessionCache.SetMode(*a.info.sessionId,
			services.ModePicCreate)
		a.handler.sessionCache.SetPicResolution(*a.info.sessionId,
			services.Resolution256)
		sendPicCreateInstructionCard(*a.ctx, a.info.sessionId,
			a.info.msgId)
		return false
	}

	mode := a.handler.sessionCache.GetMode(*a.info.sessionId)
	//fmt.Println("mode: ", mode)

	// 收到一张图片,且不在图片创作模式下, 提醒是否切换到图片创作模式
	if a.info.msgType == "image" && mode != services.ModePicCreate {
		sendPicModeCheckCard(*a.ctx, a.info.sessionId, a.info.msgId)
		return false
	}

	if a.info.msgType == "image" && mode == services.ModePicCreate {
		//保存图片
		imageKey := a.info.imageKey
		//fmt.Printf("fileKey: %s \n", imageKey)
		msgId := a.info.msgId
		//fmt.Println("msgId: ", *msgId)
		req := larkim.NewGetMessageResourceReqBuilder().MessageId(
			*msgId).FileKey(imageKey).Type("image").Build()
		resp, err := initialization.GetLarkClient().Im.MessageResource.Get(context.Background(), req)
		//fmt.Println(resp, err)
		if err != nil {
			//fmt.Println(err)
			fmt.Sprintf("🤖️：图片下载失败，请稍后再试～\n 错误信息: %v", err)
			return false
		}

		f := fmt.Sprintf("%s.png", imageKey)
		resp.WriteFile(f)
		defer os.Remove(f)
		resolution := a.handler.sessionCache.GetPicResolution(*a.
			info.sessionId)

		openai.ConvertJpegToPNG(f)
		openai.ConvertToRGBA(f, f)

		//图片校验
		err = openai.VerifyPngs([]string{f})
		if err != nil {
			replyMsg(*a.ctx, fmt.Sprintf("🤖️：无法解析图片，请发送原图并尝试重新操作～"),
				a.info.msgId)
			return false
		}
		bs64, err := a.handler.gpt.GenerateOneImageVariation(f, resolution)
		if err != nil {
			replyMsg(*a.ctx, fmt.Sprintf(
				"🤖️：图片生成失败，请稍后再试～\n错误信息: %v", err), a.info.msgId)
			return false
		}
		replayImagePlainByBase64(*a.ctx, bs64, a.info.msgId)
		return false

	}

	// 生成图片
	if mode == services.ModePicCreate {
		resolution := a.handler.sessionCache.GetPicResolution(*a.
			info.sessionId)
		bs64, err := a.handler.gpt.GenerateOneImage(a.info.qParsed,
			resolution)
		if err != nil {
			replyMsg(*a.ctx, fmt.Sprintf(
				"🤖️：图片生成失败，请稍后再试～\n错误信息: %v", err), a.info.msgId)
			return false
		}
		replayImageCardByBase64(*a.ctx, bs64, a.info.msgId, a.info.sessionId,
			a.info.qParsed)
		return false
	}

	return true
}

type SpreadsheetAction struct { /*表格*/
}

func (s *SpreadsheetAction) Execute(a *ActionInfo) bool {
	var sheetsMsg []openai.Messages
	var prompt string
	if sheetsUrl, foundSpreadsheet := utils.EitherCutPrefix(a.info.qParsed, "/sheets", "分析表格"); foundSpreadsheet {
		a.handler.sessionCache.Clear(*a.info.sessionId)
		a.handler.sessionCache.SetMode(*a.info.sessionId, services.ModeSheets)
		var err error
		sheetsMsg, err = s.BuildSheetsMsg(a, sheetsUrl)
		if err != nil {
			replyMsg(*a.ctx, err.Error(), a.info.msgId)
			return false
		}
		a.handler.sessionCache.SetMsg(*a.info.sessionId, sheetsMsg)
		go replyMsg(*a.ctx, "🤖️：表格加载成功，可以开始分析了～", a.info.msgId)
		prompt = `1.对数据进行统计分析 2.分析数据, 比较不同产品之间的差异 3.总结结果, 提炼出主要的结论。`
	} else if mode := a.handler.sessionCache.GetMode(*a.info.sessionId); mode == services.ModeSheets {
		sheetsMsg = a.handler.sessionCache.GetMsg(*a.info.sessionId)
		prompt = a.info.qParsed
	} else {
		return true
	}

	sheetsMsg = append(sheetsMsg, openai.Messages{Role: "user", Content: prompt})
	completions, err := a.handler.gpt.Completions(sheetsMsg)
	if err != nil {
		replyMsg(*a.ctx, fmt.Sprintf("🤖️：消息机器人摆烂了，请稍后再试～\n错误信息: %v", err), a.info.msgId)
		return false
	}
	err = replyMsg(*a.ctx, completions.Content, a.info.msgId)
	if err != nil {
		replyMsg(*a.ctx, fmt.Sprintf("🤖️：消息机器人摆烂了，请稍后再试～\n错误信息: %v", err), a.info.msgId)
		return false
	}
	return false
}

func (*SpreadsheetAction) ParseSpreadsheetTokenFromUrl(sheetsUrl string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(sheetsUrl))
	if err != nil {
		return "", errors.New("sheets url invalid")
	}
	paths := strings.Split(u.Path, "/")
	if len(paths) != 3 || paths[1] != "sheets" {
		return "", errors.New("sheets url invalid. path not match")
	}
	return paths[2], nil
}

func (s *SpreadsheetAction) BuildSheetsMsg(a *ActionInfo, sheetsUrl string) ([]openai.Messages, error) {
	spreadsheetToken, err := s.ParseSpreadsheetTokenFromUrl(sheetsUrl)
	if err != nil {
		return nil, errors.Errorf("🤖️：表格分析失败，请检查链接是否正确～\n错误信息: %v", err)
	}
	larkClient := initialization.GetLarkClient()

	sheesResp, err := larkClient.Sheets.SpreadsheetSheet.Query(*a.ctx, larksheets.NewQuerySpreadsheetSheetReqBuilder().SpreadsheetToken(spreadsheetToken).Build())
	if err != nil || !sheesResp.Success() {
		var errText string
		if err != nil {
			errText = err.Error()
		} else {
			errText = sheesResp.Error()
		}
		return nil, errors.Errorf("🤖️：表格获取失败～\n错误信息: %s", errText)
	}

	sheet := sheesResp.Data.Sheets[0]
	valuesResp, err := a.handler.sheets.SpreadsheetSheet.GetValues(*a.ctx, larksheetsV2.NewGetSpreadsheetSheetValuesReqBuilder().SpreadsheetToken(spreadsheetToken).Range(*sheesResp.Data.Sheets[0].SheetId).Build())
	if err != nil || !valuesResp.Success() {
		var errText string
		if err != nil {
			errText = err.Error()
		} else {
			errText = sheesResp.Error()
		}
		return nil, errors.Errorf("🤖️：表格获取失败～\n错误信息: %s", errText)
	}

	type void struct{}
	var member void
	ignoreColumns := map[string]void{
		"填写者邮箱":  member,
		"填写者部门":  member,
		"填写者 ID": member,
		"收集来源":   member,
		"提交时间":   member,
	}
	ignoreColumnsIndex := make(map[int]any, len(ignoreColumns))
	for iColumn, cell := range valuesResp.Data.ValueRange.Values[0] {
		v := strings.TrimSpace(fmt.Sprintf("%v", cell))
		if _, ok := ignoreColumns[v]; ok {
			ignoreColumnsIndex[iColumn] = member
		}
	}

	csvRecords := make([][]string, 0, len(valuesResp.Data.ValueRange.Values))
	for _, row := range valuesResp.Data.ValueRange.Values {
		newRow := make([]string, 0, len(row))
		for iColumn, cell := range row {
			if _, ok := ignoreColumnsIndex[iColumn]; ok {
				continue
			}
			v := fmt.Sprintf("%v", cell)
			if cell == nil {
				v = ""
			}
			newRow = append(newRow, v)
		}
		csvRecords = append(csvRecords, newRow)
	}
	var buf bytes.Buffer
	csvWriter := csv.NewWriter(&buf)
	csvWriter.WriteAll(csvRecords)

	return []openai.Messages{
		{Role: "system", Content: fmt.Sprintf("我希望你充当基于文本的 excel。文件名为 %s，以下CSV文本为你的数据, 第一行为表头，其他行为数据行", *sheet.Title)},
		{Role: "user", Content: buf.String()},
	}, nil
}

type MessageAction struct { /*消息*/
}

func (*MessageAction) Execute(a *ActionInfo) bool {
	msg := a.handler.sessionCache.GetMsg(*a.info.sessionId)
	msg = append(msg, openai.Messages{
		Role: "user", Content: a.info.qParsed,
	})
	completions, err := a.handler.gpt.Completions(msg)
	if err != nil {
		replyMsg(*a.ctx, fmt.Sprintf(
			"🤖️：消息机器人摆烂了，请稍后再试～\n错误信息: %v", err), a.info.msgId)
		return false
	}
	msg = append(msg, completions)
	a.handler.sessionCache.SetMsg(*a.info.sessionId, msg)
	//if new topic
	if len(msg) == 2 {
		//fmt.Println("new topic", msg[1].Content)
		sendNewTopicCard(*a.ctx, a.info.sessionId, a.info.msgId,
			completions.Content)
		return false
	}
	err = replyMsg(*a.ctx, completions.Content, a.info.msgId)
	if err != nil {
		replyMsg(*a.ctx, fmt.Sprintf(
			"🤖️：消息机器人摆烂了，请稍后再试～\n错误信息: %v", err), a.info.msgId)
		return false
	}
	return true
}

type AudioAction struct { /*语音*/
}

func (*AudioAction) Execute(a *ActionInfo) bool {
	// 只有私聊才解析语音,其他不解析
	if a.info.handlerType != UserHandler {
		return true
	}

	//判断是否是语音
	if a.info.msgType == "audio" {
		fileKey := a.info.fileKey
		//fmt.Printf("fileKey: %s \n", fileKey)
		msgId := a.info.msgId
		//fmt.Println("msgId: ", *msgId)
		req := larkim.NewGetMessageResourceReqBuilder().MessageId(
			*msgId).FileKey(fileKey).Type("file").Build()
		resp, err := initialization.GetLarkClient().Im.MessageResource.Get(context.Background(), req)
		//fmt.Println(resp, err)
		if err != nil {
			fmt.Println(err)
			return true
		}
		f := fmt.Sprintf("%s.ogg", fileKey)
		resp.WriteFile(f)
		defer os.Remove(f)

		//fmt.Println("f: ", f)
		output := fmt.Sprintf("%s.mp3", fileKey)
		// 等待转换完成
		audio.OggToWavByPath(f, output)
		defer os.Remove(output)
		//fmt.Println("output: ", output)

		text, err := a.handler.gpt.AudioToText(output)
		if err != nil {
			fmt.Println(err)

			sendMsg(*a.ctx, fmt.Sprintf("🤖️：语音转换失败，请稍后再试～\n错误信息: %v", err), a.info.msgId)
			return false
		}
		//fmt.Println("text: ", text)
		a.info.qParsed = text
		return true
	}

	return true

}
