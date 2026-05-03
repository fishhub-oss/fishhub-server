FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /app/fishhub-server .

FROM alpine:3.21

COPY --from=builder /app/fishhub-server /app/fishhub-server

CMD ["/app/fishhub-server"]
