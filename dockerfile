FROM golang:1.18 as builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

FROM ubuntu:latest  
RUN apt-get update && apt-get install -y ca-certificates stress-ng

WORKDIR /root/
COPY --from=builder /app/main .

CMD ["./main"]