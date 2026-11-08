GET {{base}}/api/status/health
HTTP 200
[Asserts]
jsonpath "$.data.status" == "ok"
jsonpath "$.data.app" exists
jsonpath "$.count" == 1
