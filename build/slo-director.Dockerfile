FROM golang:1.24.1-alpine

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/slo-director ./cmd/slo-director
RUN go build -o /usr/local/bin/slo-director ./cmd/slo-director

EXPOSE 8000

ENTRYPOINT ["/usr/local/bin/slo-director"]
