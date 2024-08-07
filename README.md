# Auth Middleware

This package provides a simple middleware for the Echo web framework that validates requests against the Samna Auth service

## Installation

```
go get github.com/yourusername/echo-middleware-example
```

## Usage

```go
import (
    "github.com/labstack/echo/v4"
    samnamw "github.com/nimling/samna-auth-middleware"
)

func main() {
    e := echo.New()
    
    // Use the middleware
    e.Use(samnamw.AuthMiddleware())
    
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