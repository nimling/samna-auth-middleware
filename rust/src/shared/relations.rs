pub const RELATION_CLAIM_OBJECTS: (&str, &str) =
    ("ClaimObjects", "claim_object.sa_deleted_at IS NULL");
pub const RELATION_CLAIM_USERS: (&str, &str) = ("ClaimUsers", "user_claim.sa_deleted_at IS NULL");
pub const RELATION_PARENT: (&str, &str) = ("Parent", "parent.sa_deleted_at IS NULL");
pub const RELATION_OWNER: (&str, &str) = ("Owner", "owner.sa_deleted_at IS NULL");
pub const RELATION_USER: (&str, &str) = ("User", "user.sa_deleted_at IS NULL");

pub fn relation_claim_objects() -> (&'static str, &'static str) {
    RELATION_CLAIM_OBJECTS
}

pub fn relation_claim_users() -> (&'static str, &'static str) {
    RELATION_CLAIM_USERS
}

pub fn relation_parent() -> (&'static str, &'static str) {
    RELATION_PARENT
}

pub fn relation_owner() -> (&'static str, &'static str) {
    RELATION_OWNER
}

pub fn relation_user() -> (&'static str, &'static str) {
    RELATION_USER
}
