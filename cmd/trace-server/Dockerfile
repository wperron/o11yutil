FROM golang:1.17 as build
WORKDIR ./app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /usr/local/bin/trace-server ./cmd/trace-server/main.go

FROM scratch
COPY --from=build /usr/local/bin/trace-server /trace-server
ENTRYPOINT ["/trace-server"]
