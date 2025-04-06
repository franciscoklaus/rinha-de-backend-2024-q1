FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .

RUN go mod download && go mod verify

RUN go build -v -o app .

FROM scratch

COPY --from=builder /app/app /app/app

CMD ["/app/app"]



