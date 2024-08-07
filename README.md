# Auth Middleware

This package provides a simple middleware for the Echo web framework that validates requests against the Samna Auth
service

## Installation

```
go get github.com/nimling/samna-auth-middleware
```

## Usage

```go
import (
    "github.com/labstack/echo/v4"
    samnamw "github.com/nimling/samna-auth-middleware"
)

func main() {
    e := echo.New()
    
    // Use the middleware, here the authURL is expected to come via the env variable SAMNA_AUTH_SERVER 
    e.Use(samnamw.AuthMiddleware())
    
    // OR with authURL specified 
    e.Use(samnamw.AuthMiddleware('https://auth.samna.io'))
    
    // OR directly on a handler 
    auth.GET("/login", samnamw.AuthMiddleware())
        
    // Your routes here
    
    e.Start(":8080")
}
```

## Testing

Run the one and only test in the package with

```go
go test
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT (Martin Invented This) License.