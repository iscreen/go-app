package interceptors

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/shoplineapp/go-app/common"
	"github.com/shoplineapp/go-app/plugins/logger"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type responseWithSecret struct {
	Secret string `json:"secret"`
	Name   string `json:"name"`
}

type otherResponseWithSecret struct {
	Secret string `json:"secret"`
}

func TestRequestLogResponseRedactAllowlist(t *testing.T) {
	originalRedactor := redactor
	t.Cleanup(func() { SetRedactor(originalRedactor) })
	r := &common.Redactor{}
	r.AddFilters(common.DefaultFilters...)
	filterCalls := 0
	r.AddFilters(common.NewFilter(&responseWithSecret{}, "secret", func(v reflect.Value) any {
		filterCalls++
		return common.FullRedact(v)
	}))
	SetRedactor(r)

	for _, tc := range []struct {
		name           string
		configurations [][]any
		response       any
		wantLog        any
		wantCalls      int
	}{
		{
			name:     "unset allowlist bypasses redaction",
			response: &responseWithSecret{Secret: "secret", Name: "merchant"},
		},
		{
			name:           "typed nil example selects response and applies custom filter",
			configurations: [][]any{{(*responseWithSecret)(nil)}},
			response:       &responseWithSecret{Secret: "secret", Name: "merchant"},
			wantLog:        map[string]any{"secret": "<REDACTED>", "name": "merchant"},
			wantCalls:      1,
		},
		{
			name:           "different response type bypasses default filters",
			configurations: [][]any{{(*responseWithSecret)(nil)}},
			response:       &testStruct{Password: "unchanged"},
		},
		{
			name:           "multiple entries support default filters",
			configurations: [][]any{{(*responseWithSecret)(nil), &testStruct{}}},
			response:       &testStruct{Name: "merchant", Password: "secret"},
			wantLog:        map[string]any{"Name": "merchant", "Age": 0, "Password": "<REDACTED>"},
		},
		{
			name:           "setter replaces previous types",
			configurations: [][]any{{(*responseWithSecret)(nil)}, {(*otherResponseWithSecret)(nil)}},
			response:       &responseWithSecret{Secret: "unchanged"},
		},
		{
			name:           "empty setter clears allowlist",
			configurations: [][]any{{(*responseWithSecret)(nil)}, {}},
			response:       &responseWithSecret{Secret: "unchanged"},
		},
		{
			name:           "untyped nil entry is ignored",
			configurations: [][]any{{nil, (*responseWithSecret)(nil)}},
			response:       &responseWithSecret{Secret: "secret", Name: "merchant"},
			wantLog:        map[string]any{"secret": "<REDACTED>", "name": "merchant"},
			wantCalls:      1,
		},
		{
			name:           "pointer and value types are distinct",
			configurations: [][]any{{(*responseWithSecret)(nil)}},
			response:       responseWithSecret{Secret: "unchanged"},
		},
		{
			name:           "allowlisted response without matching fields remains unchanged",
			configurations: [][]any{{(*otherResponseWithSecret)(nil)}},
			response:       &otherResponseWithSecret{Secret: "unchanged"},
			wantLog:        otherResponseWithSecret{Secret: "unchanged"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filterCalls = 0
			log, hook := logtest.NewNullLogger()
			i := NewGrpcRequestLogInterceptor(&logger.Logger{Logger: *log}, nil)
			// Presets can construct the handler before application startup configuration.
			interceptor := i.Handler()
			for _, types := range tc.configurations {
				i.SetResponseRedactAllowlist(types...)
			}
			// A separate instance must not inherit another instance's allowlist.
			other := NewGrpcRequestLogInterceptor(&logger.Logger{Logger: *log}, nil)
			info := &grpc.UnaryServerInfo{FullMethod: "/test.Merchant/GetMerchant"}
			handler := func(context.Context, any) (any, error) { return tc.response, nil }
			before, err := json.Marshal(tc.response)
			require.NoError(t, err)
			returned, err := interceptor(context.Background(), &testStruct{Password: "request-secret"}, info, handler)
			require.NoError(t, err)
			after, err := json.Marshal(returned)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after), "response contents must not be changed")
			require.Equal(t, tc.response, returned)
			if reflect.TypeOf(tc.response).Kind() == reflect.Pointer {
				require.Same(t, tc.response, returned)
			}
			wantLog := tc.wantLog
			if wantLog == nil {
				wantLog = tc.response
			}
			require.Equal(t, wantLog, hook.LastEntry().Data["res"])
			require.Equal(t, tc.wantCalls, filterCalls)
			require.Equal(t, "<REDACTED>", hook.LastEntry().Data["req"].(map[string]any)["Password"])
			_, err = other.Handler()(context.Background(), nil, info, handler)
			require.NoError(t, err)
			require.Equal(t, tc.response, hook.LastEntry().Data["res"])
			require.Equal(t, tc.wantCalls, filterCalls)
		})
	}
}

func TestRequestLogResponseRedactAllowlistNilAndError(t *testing.T) {
	for _, response := range []any{nil, (*responseWithSecret)(nil)} {
		for _, handlerErr := range []error{nil, errors.New("handler failed")} {
			log, hook := logtest.NewNullLogger()
			i := NewGrpcRequestLogInterceptor(&logger.Logger{Logger: *log}, nil)
			i.SetResponseRedactAllowlist((*responseWithSecret)(nil))
			returned, err := i.Handler()(context.Background(), nil,
				&grpc.UnaryServerInfo{FullMethod: "/test.Merchant/GetMerchant"},
				func(context.Context, any) (any, error) { return response, handlerErr },
			)
			require.Equal(t, response, returned)
			require.Equal(t, handlerErr, err)
			if handlerErr != nil || response == nil {
				require.NotContains(t, hook.LastEntry().Data, "res")
			} else {
				require.Equal(t, response, hook.LastEntry().Data["res"])
			}
		}
	}
}

type testStruct struct {
	Name     string
	Age      int
	Password string
}

func BenchmarkRedactor(b *testing.B) {
	s := &testStruct{Name: "JC", Age: 1, Password: "random"}
	for i := 0; i < b.N; i++ {
		redactor.Redact(s)
	}
}

func mask(data any) map[string]interface{} {
	newReq := map[string]interface{}{}
	mapReq, _ := StructToMap(data)
	for key, value := range mapReq {
		markReqParams(nil, key, value, newReq)
	}
	return newReq
}
func BenchmarkMasking(b *testing.B) {
	s := &testStruct{Name: "JC", Age: 1, Password: "random"}
	for i := 0; i < b.N; i++ {
		mask(s)
	}
}
