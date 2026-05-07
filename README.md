# Useful commands

```bash
printf 'window.APP_CONFIG = { apiUrl: "http://localhost:8081/highlight" };\n' > infra/site/config.js && python3 -m http.server 8080 --directory infra/site
```

```bash
go build -o cmd/highlight-local/rat ./cmd/rat && go run ./cmd/highlight-local
```

