go build ns2-skill .
set CGO_ENABLED=0
set GOOS=linux
go build -a -installsuffix cgo -o ns2-skill .
docker build -t tikz/ns2-skill-go .