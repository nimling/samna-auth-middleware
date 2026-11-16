use samna_auth_middleware::sauth;
use samna_auth_middleware::shared::query::claim_areas;
use samna_auth_middleware::shared::{
    ACCESS_READ, ACCESS_WRITE, AccessClaim, AccessFormat, Body, Claim, ClaimAccess, ClaimObject,
    ClaimOptions, Creator, Error, OBJECT_TYPE_USER, OBJECT_TYPE_USER_RELATION, Organization,
    ProfileItem, ProfileSchema, SamnaClient, UserClaim, UserRelation, get_code_hash,
    parse_access_value, resolve_object_type, seal_claim_options, show_claim_options,
};
use serde_json::{Value, json};
use uuid::Uuid;

#[test]
fn claim_creator_embed_json_round_trip() {
    let c = Claim {
        id: Uuid::new_v4(),
        name: "Editor".to_string(),
        access: ClaimAccess(6),
        owner_id: Uuid::new_v4(),
        root_id: Uuid::new_v4(),
        creator: Some(Creator {
            id: Uuid::new_v4(),
            user_id: Uuid::new_v4(),
            first_name: Some("Ada".to_string()),
            email: "ada@example.com".to_string(),
            ..Creator::default()
        }),
        ..Claim::default()
    };
    let raw = serde_json::to_string(&c).expect("marshal");
    assert!(
        raw.contains(r#""access":6"#),
        "expected access field on Claim, got {raw}"
    );
    assert!(
        raw.contains(r#""creator":{"#),
        "expected creator object on Claim, got {raw}"
    );
    let back: Claim = serde_json::from_str(&raw).expect("unmarshal");
    assert_eq!(back.access, ClaimAccess(6));
    assert_eq!(
        back.creator.as_ref().map(|c| c.email.as_str()),
        Some("ada@example.com")
    );
}

#[test]
fn samna_client_write_round_trip() {
    let owner = Uuid::new_v4();
    let mut c = SamnaClient {
        name: "Deploy key".to_string(),
        owner_id: owner,
        ..SamnaClient::default()
    };
    let raw = String::from_utf8(sauth::marshal(&c).expect("marshal write")).expect("utf8");
    assert!(
        raw.contains(r#""name":"Deploy key""#) && raw.contains(&format!(r#""owner_id":"{owner}""#)),
        "expected the name and the owner on the write body, got {raw}"
    );
    let (id, client_id) = (Uuid::new_v4(), Uuid::new_v4());
    let answer = format!(
        r#"{{"id":"{id}","name":"Deploy key","client_id":"{client_id}","status":"active","owner_id":"{owner}","secret":"only-once"}}"#
    );
    c = sauth::unmarshal(answer.as_bytes()).expect("unmarshal write");
    assert!(
        c.id == id && c.client_id == client_id && c.secret.as_deref() == Some("only-once"),
        "the prophet's answer did not land on the model, got {c:?}"
    );
    assert!(
        c.body.validator().is_none(),
        "a model bound from no request carries no patch body"
    );
}

#[test]
fn claim_object_no_overlay_shadows_access_bitmask() {
    let mut co = ClaimObject {
        id: Uuid::new_v4(),
        claim_id: Uuid::new_v4(),
        access: Some(ClaimAccess(4)),
        owner_id: Uuid::new_v4(),
        root_id: Uuid::new_v4(),
        ..ClaimObject::default()
    };
    let raw = serde_json::to_string(&co).expect("marshal");
    assert!(
        raw.contains(r#""access":4"#),
        "expected access bitmask on ClaimObject, got {raw}"
    );
    assert!(
        !raw.contains(r#""access_slice""#) && !raw.contains(r#""access_string""#),
        "expected no derived access fields without a format, got {raw}"
    );
    co.format_access(AccessFormat {
        slice: true,
        string: true,
    });
    let raw = serde_json::to_string(&co).expect("marshal");
    assert!(
        raw.contains(r#""access_slice":["READ"]"#) && raw.contains(r#""access_string":"READ""#),
        "expected access_slice and access_string after format_access, got {raw}"
    );
}

#[test]
fn claim_format_access_fills_requested_fields() {
    let mut c = Claim {
        id: Uuid::new_v4(),
        name: "Editor".to_string(),
        access: ACCESS_WRITE | ACCESS_READ,
        owner_id: Uuid::new_v4(),
        root_id: Uuid::new_v4(),
        ..Claim::default()
    };
    c.format_access(AccessFormat {
        slice: true,
        string: false,
    });
    let raw = serde_json::to_string(&c).expect("marshal");
    assert!(
        raw.contains(r#""access_slice":["READ","WRITE"]"#),
        "expected access_slice on Claim, got {raw}"
    );
    assert!(
        !raw.contains(r#""access_string""#),
        "expected no access_string when only slice is requested, got {raw}"
    );
}

#[test]
fn user_claim_format_access_uses_the_claim_mask() {
    let mut uc = UserClaim {
        id: Uuid::new_v4(),
        claim_id: Uuid::new_v4(),
        user_id: Uuid::new_v4(),
        owner_id: Uuid::new_v4(),
        created_by: Uuid::new_v4(),
        access: Some(Box::new(AccessClaim {
            access: ClaimAccess(4),
            owner_id: Uuid::new_v4(),
            root_id: Uuid::new_v4(),
            ..AccessClaim::default()
        })),
        creator: Some(Creator {
            id: Uuid::new_v4(),
            user_id: Uuid::new_v4(),
            email: "ada@example.com".to_string(),
            ..Creator::default()
        }),
        ..UserClaim::default()
    };
    let mask = uc.access.as_ref().map(|a| a.access).expect("access");
    uc.format_access(
        AccessFormat {
            slice: true,
            string: true,
        },
        mask,
    );
    let raw = serde_json::to_string(&uc).expect("marshal");
    assert!(
        raw.contains(r#""access":{"#),
        "expected access overlay object on UserClaim, got {raw}"
    );
    assert!(
        raw.contains(r#""access_slice":["READ"]"#),
        "expected access_slice on UserClaim, got {raw}"
    );
    assert!(
        raw.contains(r#""access_string":"READ""#),
        "expected access_string on UserClaim, got {raw}"
    );
    assert!(
        raw.contains(r#""creator":{"#),
        "expected creator overlay object on UserClaim, got {raw}"
    );
}

#[test]
fn user_relation_access_overlay_emits() {
    let r = UserRelation {
        id: Uuid::new_v4(),
        user_id: Uuid::new_v4(),
        object_type: OBJECT_TYPE_USER.into(),
        object_id: Uuid::new_v4(),
        access: Some(Box::new(AccessClaim {
            access: ClaimAccess(4),
            owner_id: Uuid::new_v4(),
            root_id: Uuid::new_v4(),
            ..AccessClaim::default()
        })),
        ..UserRelation::default()
    };
    let raw = serde_json::to_string(&r).expect("marshal");
    assert!(
        raw.contains(r#""access":{"#),
        "expected access overlay on UserRelation, got {raw}"
    );
}

#[test]
fn user_relation_object_type_constant_resolves() {
    assert_eq!(
        resolve_object_type("user_relation")
            .expect("resolve")
            .as_str(),
        OBJECT_TYPE_USER_RELATION
    );
}

#[test]
fn err_not_found_distinct_sentinel() {
    assert_eq!(Error::not_found().to_string(), "not found");
    assert!(!Error::not_found().is_no_access());
    assert!(!Error::no_access().is_not_found());
    assert!(
        Error::NotFound("mutation access".to_string()).is_not_found(),
        "a wrapped not found stays not found"
    );
}

#[test]
fn claim_access_unmarshal_json() {
    let cases: Vec<(&str, Value, Option<ClaimAccess>)> = vec![
        ("Number: ACCESS_NONE (0)", json!(0), Some(ClaimAccess(0))),
        (
            "Number: Combined WRITE and READ (6)",
            json!(6),
            Some(ACCESS_WRITE | ACCESS_READ),
        ),
        ("Number: Float error", json!(6.5), None),
        (
            "String: Single value (READ)",
            json!("READ"),
            Some(ACCESS_READ),
        ),
        (
            "String: Combined via pipe (WRITE|READ)",
            json!("WRITE|READ"),
            Some(ACCESS_WRITE | ACCESS_READ),
        ),
        (
            "String: Stringified numbers (2|4)",
            json!("2|4"),
            Some(ACCESS_WRITE | ACCESS_READ),
        ),
        ("String: unknown", json!("FLY"), None),
        (
            "Array: names",
            json!(["WRITE", " READ"]),
            Some(ACCESS_WRITE | ACCESS_READ),
        ),
        ("Object: refused", json!({"a": 1}), None),
    ];
    for (name, data, want) in cases {
        let got = parse_access_value(&data);
        match want {
            Some(w) => assert_eq!(got.expect(name), w, "{name}"),
            None => assert!(got.is_err(), "{name} must fail"),
        }
    }
}

#[test]
fn claim_areas_claim_names_itself() {
    let areas = claim_areas(
        "claimius.claim",
        &json!({"id": "20000000-0000-4000-8000-000000000001"}),
    )
    .expect("resolve claim area");
    assert_eq!(
        areas.iter().map(ToString::to_string).collect::<Vec<_>>(),
        vec!["20000000-0000-4000-8000-000000000001"]
    );
}

#[test]
fn claim_areas_binding_names_its_claim() {
    let raw = json!({"id": "30000000-0000-4000-8000-000000000009", "claim_id": "20000000-0000-4000-8000-000000000002", "user_id": "40000000-0000-4000-8000-000000000003"});
    let areas = claim_areas("claimius.user_claim", &raw).expect("resolve binding area");
    assert_eq!(
        areas.iter().map(ToString::to_string).collect::<Vec<_>>(),
        vec!["20000000-0000-4000-8000-000000000002"]
    );
}

#[test]
fn claim_areas_claim_composition_moves_both() {
    let raw = json!({"claim_id": "20000000-0000-4000-8000-000000000004", "object_type": "claimius.claim", "object_id": "20000000-0000-4000-8000-000000000005"});
    let areas = claim_areas("claimius.claim_object", &raw).expect("resolve composition areas");
    assert_eq!(
        areas.iter().map(ToString::to_string).collect::<Vec<_>>(),
        vec![
            "20000000-0000-4000-8000-000000000004",
            "20000000-0000-4000-8000-000000000005"
        ]
    );
}

#[test]
fn claim_areas_object_binding_moves_no_claim_area() {
    let raw = json!({"claim_id": "20000000-0000-4000-8000-000000000006", "object_type": "public.article", "object_id": "50000000-0000-4000-8000-000000000007"});
    assert!(
        claim_areas("claimius.claim_object", &raw)
            .expect("resolve")
            .is_empty(),
        "a claim bound to an ordinary object raises the object area, not the claim area"
    );
}

#[test]
fn claim_areas_scope_binding_moves_the_claim() {
    let raw = json!({"claim_id": "20000000-0000-4000-8000-000000000009", "scope": {"page": true}});
    let areas = claim_areas("claimius.claim_object", &raw).expect("resolve scope binding area");
    assert_eq!(
        areas.iter().map(ToString::to_string).collect::<Vec<_>>(),
        vec!["20000000-0000-4000-8000-000000000009"]
    );
}

#[test]
fn claim_areas_unrelated_event_moves_nothing() {
    assert!(
        claim_areas(
            "claimius.samna_user",
            &json!({"id": "60000000-0000-4000-8000-000000000008"})
        )
        .expect("resolve")
        .is_empty()
    );
}

fn bound_organization(body: &str) -> Organization {
    let (keys, value) = Body::bound(body.as_bytes()).expect("bind");
    let mut org: Organization = serde_json::from_value(value).expect("organization");
    org.body = keys;
    org
}

#[test]
fn organization_validate_accepts_kind() {
    bound_organization(r#"{"name":"Acme","kind":"group"}"#)
        .validate_post()
        .expect("kind accepted on create");
    bound_organization(r#"{"name":"Acme","kind":"group"}"#)
        .validate_put()
        .expect("kind accepted on update");
    bound_organization(r#"{"kind":null}"#)
        .validate_patch()
        .expect("kind cleared on patch");
}

#[test]
fn organization_validate_rejects_type() {
    for value in ["entity", "customer", "system"] {
        let body = format!(r#"{{"name":"Acme","type":"{value}"}}"#);
        for result in [
            bound_organization(&body).validate_post(),
            bound_organization(&body).validate_put(),
            bound_organization(&body).validate_patch(),
        ] {
            let err = result.expect_err("type refused");
            assert!(err.to_string().ends_with("type not allowed"), "got {err}");
        }
    }
}

#[test]
fn seal_claim_options_hashes_the_value_and_shows_it_once() {
    let (sealed, value) = seal_claim_options(&json!({"samk": {"header": true}})).expect("seal");
    assert!(
        !value.is_empty(),
        "a samk without a value is minted and handed back once"
    );
    let options: ClaimOptions = serde_json::from_value(sealed.clone()).expect("options");
    let samk = options.samk.expect("samk");
    assert!(
        samk.value.is_empty() && samk.hash == get_code_hash(&value) && samk.header,
        "the sealed options carry the sha256 of the value and never the value, got {sealed}"
    );
    let shown = show_claim_options(&sealed, &value);
    assert!(
        shown.to_string().contains(&value) && !sealed.to_string().contains(&value),
        "the value is shown on the answer and nowhere else, got {shown}"
    );
    let (chosen, plain) = seal_claim_options(&json!({"samk": {"value": "1234", "query": true}}))
        .expect("seal chosen");
    assert!(
        plain == "1234"
            && chosen.to_string().contains(&get_code_hash("1234"))
            && !chosen.to_string().contains("\"value\""),
        "a chosen value is hashed the same way, got {chosen} {plain}"
    );
    let (none, plain) = seal_claim_options(&Value::Null).expect("seal none");
    assert!(
        none.is_null() && plain.is_empty(),
        "no options seal to nothing"
    );
}

#[test]
fn profile_schema_is_a_typed_struct_with_a_fixed_vocabulary() {
    let schema: ProfileSchema = serde_json::from_str(r#"{"type": "string", "format": "uri", "component": "link", "title": "Site", "maxLength": 200}"#).expect("a schema of the vocabulary decodes");
    assert!(
        schema.r#type == "string"
            && schema.format == "uri"
            && schema.component == "link"
            && schema.max_length == Some(200),
        "the fields land typed, got {schema:?}"
    );
    assert!(
        serde_json::from_str::<ProfileSchema>(r#"{"type": "string", "widget": "spinner"}"#)
            .is_err(),
        "a key no renderer reads is refused at decode"
    );
    let cases = [
        ("a type outside the list", r#"{"type": "money"}"#),
        (
            "a format outside the list",
            r#"{"type": "string", "format": "hologram"}"#,
        ),
        (
            "a component outside the list",
            r#"{"type": "string", "component": "hologram"}"#,
        ),
        (
            "a pattern that does not compile",
            r#"{"type": "string", "pattern": "("}"#,
        ),
        (
            "items that are not a schema",
            r#"{"type": "array", "items": {"type": "money"}}"#,
        ),
        (
            "a default outside the schema",
            r#"{"type": "integer", "default": "one"}"#,
        ),
    ];
    for (name, raw) in cases {
        if let Ok(s) = serde_json::from_str::<ProfileSchema>(raw) {
            let err = s.validate("schema").expect_err(name);
            assert!(
                matches!(err, Error::BadProfileItem(_)),
                "{name} must be refused, got {err}"
            );
        }
    }
}

#[test]
fn profile_value_is_checked_against_its_schema() {
    let item = |schema: &str, value: &str| ProfileItem {
        schema: serde_json::from_str(schema).expect("schema"),
        value: serde_json::from_str(value).expect("value"),
    };
    let good = [
        item(
            r#"{"type": "string", "minLength": 1, "maxLength": 5}"#,
            r#""hello""#,
        ),
        item(
            r#"{"type": "string", "format": "uri"}"#,
            r#""https://example.no""#,
        ),
        item(r#"{"type": "integer", "minimum": 0, "maximum": 10}"#, "7"),
        item(r#"{"type": "string", "enum": ["a", "b"]}"#, r#""b""#),
        item(
            r#"{"type": "array", "items": {"type": "number"}}"#,
            "[1, 2.5]",
        ),
        item(
            r#"{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}"#,
            r#"{"name": "x"}"#,
        ),
        item(r#"{"type": "boolean", "component": "boolean"}"#, "true"),
        item(r#"{"type": "null"}"#, "null"),
    ];
    for (n, i) in good.iter().enumerate() {
        i.validate()
            .unwrap_or_else(|e| panic!("case {n} must pass, got {e}"));
    }
    let bad = [
        item(r#"{"type": "string", "maxLength": 3}"#, r#""hello""#),
        item(
            r#"{"type": "string", "format": "image"}"#,
            r#""http://example.no/a.png""#,
        ),
        item(r#"{"type": "integer"}"#, "7.5"),
        item(r#"{"type": "number", "maximum": 1}"#, "2"),
        item(r#"{"type": "string", "enum": ["a"]}"#, r#""b""#),
        item(
            r#"{"type": "array", "items": {"type": "number"}}"#,
            r#"[1, "two"]"#,
        ),
        item(r#"{"type": "object", "required": ["name"]}"#, "{}"),
        item(r#"{"type": "string", "pattern": "^[a-z]+$"}"#, r#""A1""#),
        item(r#"{"type": "boolean"}"#, r#""yes""#),
    ];
    for (n, i) in bad.iter().enumerate() {
        let err = i
            .validate()
            .expect_err(&format!("case {n} must be refused"));
        assert!(
            matches!(err, Error::BadProfileItem(_)),
            "case {n} got {err}"
        );
    }
}
