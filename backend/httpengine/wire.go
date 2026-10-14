package httpengine

import (
	"net/http"
	"sort"

	"apirequest/backend/model"
)

// captureWire 从最终响应回指的 Request 提取实际发出的请求摘要。
// Go transport 自动跟随重定向时 resp.Request 即最后一次发出的请求，
// 因此重定向链的落点无需额外采集；头按字母排序保证输出稳定。
func captureWire(resp *http.Response) *model.WireDump {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return nil
	}
	req := resp.Request
	headers := make([]model.KV, 0, len(req.Header))
	for key, values := range req.Header {
		for _, value := range values {
			headers = append(headers, model.KV{Key: key, Value: value})
		}
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].Key < headers[j].Key })
	return &model.WireDump{
		Method:  req.Method,
		Url:     req.URL.String(),
		Headers: headers,
	}
}
