package gorbitaltest_test

import "testing"

// TestExamples runs the test bodies of the examples, so what they claim
// holds against a real app and database.
func TestExamples(t *testing.T) {
	for _, ex := range []struct {
		name string
		run  func()
	}{
		{"ExampleNew", ExampleNew},
		{"ExampleApp", ExampleApp},
		{"ExampleApp_App", ExampleApp_App},
		{"ExampleApp_Client", ExampleApp_Client},
		{"ExampleApp_As", ExampleApp_As},
		{"ExampleUser", ExampleUser},
		{"ExampleAPIKey", ExampleAPIKey},
		{"ExampleClient", ExampleClient},
		{"ExampleClient_WithHeader", ExampleClient_WithHeader},
		{"ExampleClient_Get", ExampleClient_Get},
		{"ExampleClient_Post", ExampleClient_Post},
		{"ExampleClient_Put", ExampleClient_Put},
		{"ExampleClient_Patch", ExampleClient_Patch},
		{"ExampleClient_Delete", ExampleClient_Delete},
		{"ExampleClient_Do", ExampleClient_Do},
		{"ExampleResponse", ExampleResponse},
		{"ExampleResponse_JSON", ExampleResponse_JSON},
		{"ExampleResponse_AssertStatus", ExampleResponse_AssertStatus},
		{"ExampleResponse_AssertProblem", ExampleResponse_AssertProblem},
		{"ExampleJob", ExampleJob},
		{"ExampleApp_Jobs", ExampleApp_Jobs},
		{"ExampleApp_Mail", ExampleApp_Mail},
	} {
		bodies = nil
		ex.run()
		for _, body := range bodies {
			t.Run(ex.name, body)
		}
	}
}
