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
    
    // Use the middleware, here values is expected to come from env, this is the same as calling samnamw.AuthMiddlewareFromEnv()
    e.Use(samnamw.AuthMiddleware(nil, nil, nil))
    
    // OR with inline  
    e.Use(samnamw.AuthMiddleware('https://auth.samna.io', '00000000-0000-0000-0000-000000000000', 'your-client-secret'))
    
    // OR directly on a handler 
    auth.GET("/login", samnamw.AuthMiddlewareFromEnv())
        
    // Your routes here
    
    e.Start(":8080")
}
```
Example of a `.env` file with required env variables to run the AuthMiddleware without inline configurations
```env
SAMNA_AUTH_SERVER=http://localhost:3200
SAMNA_AUTH_CLIENT_ID=00000000-0000-0000-0000-000000000000
SAMNA_AUTH_CLIENT_SECRET=your-client-secret
```

There is also an included mini demo server as an example at [/example](./example), this runs live-testing against a 
running SamnaAuth server when configured with a valid `.env` file in the same directory. 

## Testing

Run the one and only test in the package with

```go
go test
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT (Martin Invented This) License.