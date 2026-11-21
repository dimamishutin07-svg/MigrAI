FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /migr-ai ./cmd/bot

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /migr-ai /migr-ai
COPY schema.sql /app/schema.sql
COPY knowledge /app/knowledge
EXPOSE 8080
ENTRYPOINT ["/migr-ai"]
