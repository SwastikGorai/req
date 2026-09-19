package execution

import (
	"github.com/SwastikGorai/req/internal/model"
	"testing"
)

func TestPrepareRejectsUnresolvedQueryAndInvalidJSON(t *testing.T) {
	for _, req := range []model.Request{
		{Query: []model.Entry{{Key: "q", Value: "{{missing}}", Enabled: true}}},
		{Query: []model.Entry{{Key: "{{missing}}", Value: "q", Enabled: true}}},
		{Body: &model.Body{Type: "json", Text: &[]string{"invalid"}[0]}},
	} {
		req.Method, req.URL = "GET", "http://localhost/"
		if _, err := Prepare(req, Overrides{}, Policy{}); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
}
