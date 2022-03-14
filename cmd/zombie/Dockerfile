FROM golang:1.17 as build
WORKDIR ./app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /usr/local/bin/zombie ./cmd/zombie/main.go

FROM scratch
ENV PATH=/
COPY --from=build /usr/local/bin/zombie /zombie
CMD [ "./zombie", "-config", "/zombie.yaml" ]