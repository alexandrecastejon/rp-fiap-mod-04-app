# Build: dependências exatamente como no go.sum (download + verify, sem "go mod tidy")
FROM golang:1.26-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/main .

# Runtime distroless: sem shell nem gerenciador de pacotes, usuário não-root (65532)
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=builder /out/main /app/main
USER 65532:65532
EXPOSE 8004
ENTRYPOINT ["/app/main"]
