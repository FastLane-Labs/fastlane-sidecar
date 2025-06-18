FROM golang:1.23

RUN apt-get update && apt-get install -y libsystemd-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /fastlane-sidecar .

ENTRYPOINT ["/fastlane-sidecar"]
