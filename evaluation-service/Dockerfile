FROM golang:1.26-alpine AS builder
WORKDIR /app
RUN apk update && apk upgrade --no-cache
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates curl
COPY --from=builder /app/main .
EXPOSE 8004
ENTRYPOINT ["./main"]