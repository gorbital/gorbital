package delivery

import (
	"encoding/json"
	"reflect"
	"testing"
)

type fuzzFields struct {
	DisplayName string   `json:"display_name"`
	Country     string   `json:"country,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Age         int      `json:"age,omitempty"`
}

// FuzzRegisterBody: decoding a registration body with extra fields never
// panics, and decodes email, password and the fields exactly as decoding
// them separately does, so the fields can't change sign-in's own values.
func FuzzRegisterBody(f *testing.F) {
	for _, seed := range []string{
		`{"email":"ada@example.com","password":"correct horse battery","display_name":"Ada","country":"GB"}`,
		`{"email":"a","password":"b","EMAIL":"c","Display_Name":"x","tags":["a"],"age":3,"extra":{"nested":[1,2]}}`,
		`{"display_name":null,"email":1}`,
		`[]`, `null`, `{"email":"\ud800"}`, `{`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var body RegisterBody[fuzzFields]
		err := json.Unmarshal(data, &body)

		var base registerBase
		baseErr := json.Unmarshal(data, &base)
		var fields fuzzFields
		fieldsErr := json.Unmarshal(data, &fields)
		if (err == nil) != (baseErr == nil && fieldsErr == nil) {
			t.Fatalf("Unmarshal(%q) error = %v; separately %v, %v", data, err, baseErr, fieldsErr)
		}
		if err != nil {
			return
		}
		if body.Email != base.Email || body.Password != base.Password || !reflect.DeepEqual(body.Fields, fields) {
			t.Fatalf("Unmarshal(%q) = %+v; separately %+v %+v", data, body, base, fields)
		}
	})
}
