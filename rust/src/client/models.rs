use std::fmt;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ErrorKind {
    BadRequest,
    Conflict,
    Forbidden,
    NotHome,
    NotFound,
    Unauthorized,
    Other,
}

#[derive(Clone, Debug)]
pub struct ClientError {
    pub kind: ErrorKind,
    pub message: String,
}

impl ClientError {
    pub fn new(kind: ErrorKind, message: impl Into<String>) -> Self {
        ClientError {
            kind,
            message: message.into(),
        }
    }

    pub fn bad_request() -> Self {
        ClientError::new(ErrorKind::BadRequest, "bad request")
    }

    pub fn conflict() -> Self {
        ClientError::new(ErrorKind::Conflict, "conflict: bad request")
    }

    pub fn forbidden() -> Self {
        ClientError::new(ErrorKind::Forbidden, "forbidden")
    }

    pub fn not_home(home: &str) -> Self {
        ClientError::new(
            ErrorKind::NotHome,
            format!("not the home prophet: conflict: bad request, the prophet is {home}"),
        )
    }

    pub fn not_found() -> Self {
        ClientError::new(ErrorKind::NotFound, "not found")
    }

    pub fn unauthorized() -> Self {
        ClientError::new(ErrorKind::Unauthorized, "unauthorized")
    }

    pub fn other(message: impl Into<String>) -> Self {
        ClientError::new(ErrorKind::Other, message)
    }

    pub fn is(&self, kind: ErrorKind) -> bool {
        match kind {
            ErrorKind::BadRequest => matches!(
                self.kind,
                ErrorKind::BadRequest | ErrorKind::Conflict | ErrorKind::NotHome
            ),
            ErrorKind::Conflict => matches!(self.kind, ErrorKind::Conflict | ErrorKind::NotHome),
            other => self.kind == other,
        }
    }
}

impl fmt::Display for ClientError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for ClientError {}
