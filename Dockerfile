FROM    golang:1.19.3 as base
WORKDIR /app

COPY    . ./
RUN     go mod download

RUN     go build -o app

EXPOSE  8080

CMD     ["./app"]