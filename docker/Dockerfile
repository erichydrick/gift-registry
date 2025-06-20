FROM golang:1.24-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o main cmd/api/main.go

FROM alpine:3.21.3 AS executor

WORKDIR /app

COPY --from=build /app/main /app/main
ADD internal/database/migrations migrations/
COPY internal/database/private_migrations/* migrations/
ADD cmd/web/templates/ templates/
ADD cmd/web/css/ css/
ADD cmd/web/js/ js/
CMD ["./main"]
