FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY *.go ./
RUN go build -ldflags='-s -w' -o fal-cli .

FROM alpine:latest
RUN apk add --no-cache ffmpeg
COPY --from=builder /app/fal-cli /usr/local/bin/fal-cli
ENV PORT=8080
EXPOSE 8080
CMD ["fal-cli", "serve"]
