package v2

import (
	"context"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"net/http"
	"start-feishubot/initialization"
)

func newService(config *larkcore.Config) *SheetsService {
	s := &SheetsService{config: config}
	s.SpreadsheetSheet = &spreadsheetSheet{service: s}
	return s
}

type SheetsService struct {
	config           *larkcore.Config
	SpreadsheetSheet *spreadsheetSheet // 单元格
}

type spreadsheetSheet struct {
	service *SheetsService
}

func (s *spreadsheetSheet) GetValues(ctx context.Context, req *GetSpreadsheetSheetValuesReq, options ...larkcore.RequestOptionFunc) (*GetSpreadsheetSheetValuesResp, error) {
	apiReq := req.apiReq
	apiReq.ApiPath = "/open-apis/sheets/v2/spreadsheets/:spreadsheet_token/values/:range"
	apiReq.HttpMethod = http.MethodGet
	apiReq.SupportedAccessTokenTypes = []larkcore.AccessTokenType{larkcore.AccessTokenTypeTenant, larkcore.AccessTokenTypeUser}
	apiResp, err := larkcore.Request(ctx, apiReq, s.service.config, options...)
	if err != nil {
		return nil, err
	}
	// 反序列响应结果
	resp := &GetSpreadsheetSheetValuesResp{ApiResp: apiResp}
	err = apiResp.JSONUnmarshalBody(resp, s.service.config)
	if err != nil {
		return nil, err
	}
	return resp, err
}

// models

type GetSpreadsheetSheetValuesReqBuilder struct {
	apiReq *larkcore.ApiReq
}

func NewGetSpreadsheetSheetValuesReqBuilder() *GetSpreadsheetSheetValuesReqBuilder {
	builder := &GetSpreadsheetSheetValuesReqBuilder{}
	builder.apiReq = &larkcore.ApiReq{
		PathParams:  larkcore.PathParams{},
		QueryParams: larkcore.QueryParams{},
	}
	return builder
}

// 电子表格的token
//
// 示例值：shtxxxxxxxxxxxxxxxx
func (builder *GetSpreadsheetSheetValuesReqBuilder) SpreadsheetToken(spreadsheetToken string) *GetSpreadsheetSheetValuesReqBuilder {
	builder.apiReq.PathParams.Set("spreadsheet_token", fmt.Sprint(spreadsheetToken))
	return builder
}

func (builder *GetSpreadsheetSheetValuesReqBuilder) Range(range_ string) *GetSpreadsheetSheetValuesReqBuilder {
	builder.apiReq.PathParams.Set("range", range_)
	return builder
}

func (builder *GetSpreadsheetSheetValuesReqBuilder) Build() *GetSpreadsheetSheetValuesReq {
	req := &GetSpreadsheetSheetValuesReq{}
	req.apiReq = &larkcore.ApiReq{}
	req.apiReq.PathParams = builder.apiReq.PathParams
	return req
}

type GetSpreadsheetSheetValuesReq struct {
	apiReq *larkcore.ApiReq
}

type GetSpreadsheetSheetValuesResp struct {
	*larkcore.ApiResp `json:"-"`
	larkcore.CodeError
	Data *GetSpreadsheetSheetValuesRespData `json:"data"` // 业务数据
}

func (resp *GetSpreadsheetSheetValuesResp) Success() bool {
	return resp.Code == 0
}

type GetSpreadsheetSheetValuesRespData struct {
	Revision         int    `json:"revision"`
	SpreadsheetToken string `json:"spreadsheetToken"`
	ValueRange       struct {
		MajorDimension string  `json:"majorDimension"`
		Range          string  `json:"range"`
		Revision       int     `json:"revision"`
		Values         [][]any `json:"values"`
	}
}

func NewConfig(appId, appSecret string, options ...lark.ClientOptionFunc) *larkcore.Config {
	// 构建配置
	config := &larkcore.Config{
		BaseUrl:          lark.FeishuBaseUrl,
		AppId:            appId,
		AppSecret:        appSecret,
		EnableTokenCache: true,
		AppType:          larkcore.AppTypeSelfBuilt,
	}
	for _, option := range options {
		option(config)
	}

	// 构建日志器
	larkcore.NewLogger(config)

	// 构建缓存
	larkcore.NewCache(config)

	// 创建序列化器
	larkcore.NewSerialization(config)

	// 创建httpclient
	larkcore.NewHttpClient(config)
	return config
}

func NewService(config initialization.Config) *SheetsService {
	return newService(NewConfig(config.FeishuAppId, config.FeishuAppSecret))
}
