package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

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

	payload, status := highlightapi.HandleRequest(strings.NewReader(body), "./rat")
	return respond(status, payload), nil
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
