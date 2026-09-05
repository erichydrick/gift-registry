FROM golang:1.27.1-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o main cmd/api/main.go

FROM alpine:3.22.2 AS executor

WORKDIR /app

RUN apk add --no-cache libgcc gcompat

COPY --from=build /app/main /app/main
COPY internal/database/migrations migrations/
COPY cmd/web/templates/ templates/
COPY cmd/web/css/ css/
COPY cmd/web/js/ js/
CMD ["./main"]
