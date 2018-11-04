# ns2-skill-go
This is a complete rewrite of ns2-skill in Go, originally written in Python. This is the production version at NS2SUD.

### How fast it is compared to the previous one?
Without accounting for database latency and query execution time, running in AWS EC2 t2.micro instance, an 8v8 shuffle with ~12k combinations:

- **Python**: anywhere between 1 and 10 seconds.
- **Go**: 70ms in the worst case, usually <10 ms.

## Deploy
`go build ns2-skill-go` yourself, or just run the provided binaries for Windows or Linux.
You will need to set the following environment variables before:
```
MYSQL_HOST = "example.com:3306"
MYSQL_USER = "user"
MYSQL_PASS = "pass"
MYSQL_DB = "database"
```

Minimal Docker image is available at `tikz/ns2-skill-go`