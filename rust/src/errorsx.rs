use http::StatusCode;

use crate::shared::Error;

pub fn err_to_http_code(err: &Error) -> StatusCode {
    match err {
        Error::NotFound(_) => StatusCode::NOT_FOUND,
        Error::Internal(_) => StatusCode::INTERNAL_SERVER_ERROR,
        Error::BadRequest(_) => StatusCode::BAD_REQUEST,
        Error::AlreadyExists(_) => StatusCode::CONFLICT,
        Error::NoAccess(_) => StatusCode::FORBIDDEN,
        Error::Postgres(e) => match e.code().map(|c| c.code()) {
            Some("P0403") => StatusCode::FORBIDDEN,
            Some("P0404") => StatusCode::NOT_FOUND,
            _ => StatusCode::INTERNAL_SERVER_ERROR,
        },
        _ => StatusCode::INTERNAL_SERVER_ERROR,
    }
}

pub fn is_unexpected(err: &Error) -> bool {
    matches!(err, Error::Internal(_))
}
