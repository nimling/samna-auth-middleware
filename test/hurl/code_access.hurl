GET {{base}}/api/root?samk=hurl-guest-code
Accept: text/html
HTTP 303
[Asserts]
header "Location" == "/api/root"
header "Referrer-Policy" == "no-referrer"
header "Set-Cookie" not exists

GET {{base}}/code
SAuth-Code: hurl-guest-code
HTTP 200
[Asserts]
header "Referrer-Policy" == "no-referrer"
jsonpath "$.data.kind" == "guest"
jsonpath "$.data.object_type" == "claimius.organization"
jsonpath "$.data.data.view" == "board"
jsonpath "$.data.hash" not exists
body not contains "hurl-guest-code"

GET {{base}}/code
HTTP 404

GET {{base}}/api/root?samk=not-a-code
Accept: text/html
HTTP 401
[Asserts]
header "Referrer-Policy" == "no-referrer"

GET {{base}}/api/root
HTTP 200
[Asserts]
header "Referrer-Policy" not exists

GET {{base}}/api/root?samk=hurl-invite-code
Accept: text/html
HTTP 303
[Asserts]
header "Location" == "/api/root"
header "Referrer-Policy" == "no-referrer"

GET {{base}}/api/root?samk=hurl-invite-code
Accept: text/html
HTTP 303
[Asserts]
header "Location" == "/api/root"
