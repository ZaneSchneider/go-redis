FROM golang:1.26 AS builder

WORKDIR /src

COPY go.mod ./
COPY . . 

RUN CGO_ENABLED=0 GOOS=linux go build -o /redis-server ./app

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /redis-server /redis-server

EXPOSE 6379

ENTRYPOINT ["/redis-server"] 