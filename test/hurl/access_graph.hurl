GET {{base}}/api/access/organization/{{grand}}?required=2
HTTP 200
[Asserts]
jsonpath "$.claim_name" == "Root writer"
jsonpath "$.root_id" == "{{root}}"

GET {{base}}/api/access/organization/{{grand}}?required=1
HTTP 403
[Asserts]
jsonpath "$.error" == "no access"

GET {{base}}/api/access/organization/{{missing}}?required=4
HTTP 404
[Asserts]
jsonpath "$.error" == "not found"

POST {{base}}/api/access
[
  {"object_id": "{{grand}}", "object_type": "claimius.organization", "required": 2},
  {"object_id": "{{alpha}}", "object_type": "claimius.organization", "required": 1}
]
HTTP 200
[Asserts]
jsonpath "$[0].object_id" == "{{grand}}"
jsonpath "$[0].granted" == true
jsonpath "$[0].claim_name" == "Root writer"
jsonpath "$[1].object_id" == "{{alpha}}"
jsonpath "$[1].granted" == false

GET {{base}}/api/user/ext-a
HTTP 200
[Asserts]
jsonpath "$.target_user_id" == "{{user_a}}"

GET {{base}}/api/user/{{user_a}}
HTTP 200
[Asserts]
jsonpath "$.target_user_id" == "{{user_a}}"

GET {{base}}/api/root
HTTP 200
[Asserts]
jsonpath "$.root_id" == "{{root}}"

GET {{base}}/api/graph/owner
HTTP 200
[Asserts]
jsonpath "$[*].object_id" contains "{{root}}"
jsonpath "$[*].object_id" contains "{{alpha}}"
jsonpath "$[*].object_id" contains "{{bravo}}"
jsonpath "$[*].object_id" contains "{{charlie}}"
jsonpath "$[*].object_id" contains "{{grand}}"

GET {{base}}/api/graph/claim
HTTP 200
[Asserts]
jsonpath "$[?(@.object_type=='claimius.claim')].label" contains "Root writer"
jsonpath "$[?(@.object_type=='claimius.claim')].label" contains "Alpha reader"

GET {{base}}/api/organizations?limit=2
HTTP 200
[Asserts]
jsonpath "$" count == 2
jsonpath "$[0].id" < "{{bravo}}"

GET {{base}}/api/organizations?cursor={{alpha}}&limit=10
HTTP 200
[Asserts]
jsonpath "$" count == 3
jsonpath "$[*].id" contains "{{bravo}}"
jsonpath "$[*].id" contains "{{charlie}}"
jsonpath "$[*].id" contains "{{grand}}"
jsonpath "$[*].id" not contains "{{root}}"

GET {{base}}/api/organizations
HTTP 200
[Asserts]
jsonpath "$" count == 5

GET {{base}}/api/locations?limit=1
HTTP 200
[Asserts]
jsonpath "$" count == 1
jsonpath "$[0].id" == "{{hall}}"

GET {{base}}/api/locations?cursor={{hall}}&limit=5
HTTP 200
[Asserts]
jsonpath "$" count == 1
jsonpath "$[0].id" == "{{wing}}"
