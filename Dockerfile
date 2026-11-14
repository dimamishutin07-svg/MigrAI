FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /migr-ai ./cmd/bot
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /sourceparser ./cmd/sourceparser

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /migr-ai /migr-ai
COPY --from=build /sourceparser /sourceparser
COPY schema.sql /app/schema.sql
COPY knowledge_schema.sql /app/knowledge_schema.sql
EXPOSE 8080
ENTRYPOINT ["/migr-ai"]
