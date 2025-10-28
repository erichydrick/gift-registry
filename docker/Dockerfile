FROM golang:1.24-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o main cmd/api/main.go

FROM scratch AS executor

WORKDIR /app

COPY --from=build /app/main /app/main
COPY internal/database/migrations migrations/
COPY internal/database/private_migrations migrations/
COPY cmd/web/templates/ templates/
COPY cmd/web/css/ css/
COPY cmd/web/js/ js/
CMD ["./main"]
