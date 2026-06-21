module rat

go 1.26.0

require (
	github.com/aws/aws-lambda-go v1.47.0
	github.com/stretchr/testify v1.10.0
)

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
)

replace github.com/stretchr/testify => ./third_party/testify
