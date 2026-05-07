package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"rat/internal/highlightapi"
)

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	_ = ctx
	body := req.Body
	if req.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return respond(http.StatusBadRequest, highlightapi.ResponseBody{Error: "invalid base64 body"}), nil
		}
		body = string(decoded)
	}

	var payload highlightapi.RequestBody
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return respond(http.StatusBadRequest, highlightapi.ResponseBody{Error: "invalid JSON payload"}), nil
	}

	html, status, err := highlightapi.Process(payload.GithubURL, "./rat")
	if err != nil {
		return respond(status, highlightapi.ResponseBody{Error: err.Error()}), nil
	}
	return respond(http.StatusOK, highlightapi.ResponseBody{HTML: html}), nil
}

func respond(code int, payload highlightapi.ResponseBody) events.APIGatewayProxyResponse {
	body, _ := json.Marshal(payload)
	return events.APIGatewayProxyResponse{
		StatusCode: code,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(body),
	}
}

func main() {
	lambda.Start(handler)
}
