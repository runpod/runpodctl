FROM golang:1.23 AS builder
RUN apt-get update
# do the thing with cache directories from my article, I'll do it later
WORKDIR /app

COPY main.go main.go
COPY go.mod go.mod  
COPY go.sum go.sum
RUN --mount=type=cache,target=/go/pkg/mod/ go mod download 
COPY . .
# build our application
RUN --mount=type=cache,target=/root/.cache/go-build GOOS=linux GOARCH=amd64 go build -o runpodctl .
RUN --mount=type=cache,target=/root/.cache/go-build GOOS=linux GOARCH=amd64 go build -o configure-github-action ./cmd/configure-github-action

FROM ubuntu:latest AS runner
COPY --from=builder /app/runpodctl /app/runpodctl
COPY --from=builder /app/configure-github-action /app/configure-github-action  
