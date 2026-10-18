# Build: dependências exatamente como no go.sum (download + verify, sem "go mod tidy")
FROM golang:1.27-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS builder
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
