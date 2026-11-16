use crate::client::{ClientError, ErrorKind};

#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("no access{}", detail(.0))]
    NoAccess(String),
    #[error("not found{}", detail(.0))]
    NotFound(String),
    #[error("internal server error{}", detail(.0))]
    Internal(String),
    #[error("bad request{}", detail(.0))]
    BadRequest(String),
    #[error("already exists{}", detail(.0))]
    AlreadyExists(String),
    #[error("identity already registered under a different root")]
    RootConflict,
    #[error("user already exists")]
    UserAlreadyExists,
    #[error(
        "an organization moves only under an organization of its own root that is neither itself nor one of its descendants"
    )]
    OrganizationParent,
    #[error("the customer organization cannot be deleted")]
    OrganizationCustomer,
    #[error("sync unreachable: app inactive on samna_auth")]
    SyncUnreachable,
    #[error(
        "claimius.disciple_state has no row: the service must call claimius.upsert_disciple_state at setup"
    )]
    NoDiscipleState,
    #[error("secret expired")]
    SecretExpired,
    #[error(
        "a routed update carries a Set expression the prophet cannot apply: set the field on the model, or use NewDelete for a soft delete: {0}"
    )]
    RoutedSet(String),
    #[error(
        "a routed update or delete carries no record: populate the model's key or call ID, the record is never read from a Where expression: {0}"
    )]
    RoutedNoRecord(String),
    #[error("database not connected: call sdb.Init first")]
    NotConnected,
    #[error("code is unknown, revoked, outside its window or used up")]
    CodeInvalid,
    #[error("bad code request{}", detail(.0))]
    BadCode(String),
    #[error("bad cost{}", detail(.0))]
    BadAppCost(String),
    #[error("bad profile item{}", detail(.0))]
    BadProfileItem(String),
    #[error("bad user name{}", detail(.0))]
    BadUserName(String),
    #[error("user name taken{}", detail(.0))]
    UserNameTaken(String),
    #[error("no prophet to send the profile item to")]
    NoProphet,
    #[error("sync cursor refused by the prophet feed, snapshot required")]
    SnapshotRequired,
    #[error("{0}")]
    Client(ClientError),
    #[error("{0}")]
    Postgres(#[from] tokio_postgres::Error),
    #[error("{0}")]
    Pool(String),
    #[error("{0}")]
    Json(#[from] serde_json::Error),
    #[error("{0}")]
    Other(String),
}

fn detail(s: &str) -> String {
    if s.is_empty() {
        String::new()
    } else {
        format!(": {s}")
    }
}

pub type Result<T> = std::result::Result<T, Error>;

impl Error {
    pub fn no_access() -> Self {
        Error::NoAccess(String::new())
    }

    pub fn not_found() -> Self {
        Error::NotFound(String::new())
    }

    pub fn internal() -> Self {
        Error::Internal(String::new())
    }

    pub fn other(message: impl Into<String>) -> Self {
        Error::Other(message.into())
    }

    pub fn bad_request(message: impl Into<String>) -> Self {
        Error::BadRequest(message.into())
    }

    pub fn is_no_access(&self) -> bool {
        matches!(self, Error::NoAccess(_))
            || matches!(self, Error::Client(c) if c.is(ErrorKind::Forbidden))
    }

    pub fn is_not_found(&self) -> bool {
        matches!(self, Error::NotFound(_))
            || matches!(self, Error::Client(c) if c.is(ErrorKind::NotFound))
    }

    pub fn is_not_home(&self) -> bool {
        matches!(self, Error::Client(c) if c.is(ErrorKind::NotHome))
    }

    pub fn is_conflict(&self) -> bool {
        matches!(self, Error::Client(c) if c.is(ErrorKind::Conflict))
    }

    pub fn is_bad_request(&self) -> bool {
        matches!(self, Error::BadRequest(_))
            || matches!(self, Error::Client(c) if c.is(ErrorKind::BadRequest))
    }

    pub fn is_unauthorized(&self) -> bool {
        matches!(self, Error::Client(c) if c.is(ErrorKind::Unauthorized))
    }

    pub fn is_no_rows(&self) -> bool {
        matches!(self, Error::NotFound(s) if s == "no rows")
    }

    pub fn pg_code(&self) -> Option<&str> {
        match self {
            Error::Postgres(e) => e.code().map(|c| c.code()),
            _ => None,
        }
    }

    pub fn pg_message(&self) -> String {
        match self {
            Error::Postgres(e) => e
                .as_db_error()
                .map(|d| d.message().to_string())
                .unwrap_or_else(|| e.to_string()),
            other => other.to_string(),
        }
    }

    pub fn no_rows() -> Self {
        Error::NotFound("no rows".to_string())
    }

    pub fn claimius(self) -> Self {
        match self.pg_code() {
            Some("P0403") => Error::NoAccess(self.pg_message()),
            Some("P0404") => Error::NotFound(self.pg_message()),
            _ => self,
        }
    }
}

impl From<ClientError> for Error {
    fn from(e: ClientError) -> Self {
        Error::Client(e)
    }
}

impl From<deadpool_postgres::PoolError> for Error {
    fn from(e: deadpool_postgres::PoolError) -> Self {
        Error::Pool(e.to_string())
    }
}

impl From<String> for Error {
    fn from(s: String) -> Self {
        Error::Other(s)
    }
}

impl From<&str> for Error {
    fn from(s: &str) -> Self {
        Error::Other(s.to_string())
    }
}
