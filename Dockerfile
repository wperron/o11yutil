FROM golang:1.17 as build
WORKDIR ./app
COPY . .
RUN CGO_ENABLED=0 GO_OS=linux GO_ARCH=amd64 go build -o /usr/local/bin/slowpoke ./main.go

FROM scratch
COPY --from=build /usr/local/bin/slowpoke /slowpoke
CMD ["/slowpoke", "-addr=:8080", "-trace=tempo:55680"]