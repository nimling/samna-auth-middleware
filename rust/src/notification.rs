use std::collections::HashMap;
use std::convert::Infallible;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::Duration;

use axum::response::sse::{Event, KeepAlive, Sse};
use axum::response::{IntoResponse, Response};
use serde_json::Value;
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

use crate::shared::{Ctx, Db, Error, Result, resolve_object_type};

const CHANNEL: &str = "claimius_notification";
const HEARTBEAT: Duration = Duration::from_secs(30);
const JOB_BUFFER: usize = 1024;
const CLIENT_BUFFER: usize = 256;
const DRAIN_TIMEOUT: Duration = Duration::from_secs(10);

#[derive(Clone, Debug, Default)]
pub struct Notification {
    pub title: String,
    pub description: Option<String>,
    pub style: Value,
    pub r#type: String,
    pub link: Option<String>,
    pub app_id: Option<Uuid>,
    pub created_by: Option<Uuid>,
    pub owner_id: Option<Uuid>,
    pub repeats_at: Option<String>,
    pub object_type: String,
    pub object_id: Option<Uuid>,
}

struct Job {
    input: Notification,
    object_type: Option<String>,
    object_id: Option<Uuid>,
    user_ids: Vec<Uuid>,
}

struct Hub {
    clients: Mutex<HashMap<Uuid, Vec<mpsc::Sender<Vec<u8>>>>>,
}

impl Hub {
    fn register(&self, user_id: Uuid) -> mpsc::Receiver<Vec<u8>> {
        let (tx, rx) = mpsc::channel(CLIENT_BUFFER);
        self.clients
            .lock()
            .expect("hub")
            .entry(user_id)
            .or_default()
            .push(tx);
        rx
    }

    fn has(&self, user_id: Uuid) -> bool {
        self.clients
            .lock()
            .expect("hub")
            .get(&user_id)
            .is_some_and(|c| !c.is_empty())
    }

    fn push(&self, user_id: Uuid, data: Vec<u8>) {
        let mut held = self.clients.lock().expect("hub");
        if let Some(clients) = held.get_mut(&user_id) {
            clients.retain(|c| !c.is_closed());
            for c in clients.iter() {
                let _ = c.try_send(data.clone());
            }
        }
    }

    fn close_all(&self) {
        self.clients.lock().expect("hub").clear();
    }
}

struct Runner {
    db: &'static Db,
    hub: Arc<Hub>,
    jobs: mpsc::Sender<Job>,
    stop: CancellationToken,
    closed: Mutex<bool>,
}

static RUN: OnceLock<Mutex<Option<Arc<Runner>>>> = OnceLock::new();
static SERVE: OnceLock<CancellationToken> = OnceLock::new();

fn serve_token() -> &'static CancellationToken {
    SERVE.get_or_init(CancellationToken::new)
}

pub fn end_streams() {
    serve_token().cancel();
}

fn current() -> Option<Arc<Runner>> {
    RUN.get()
        .and_then(|r| r.lock().expect("notification").clone())
}

pub async fn init(stop: CancellationToken, db: &'static Db) -> Result<()> {
    let slot = RUN.get_or_init(|| Mutex::new(None));
    if let Some(run) = slot.lock().expect("notification").as_ref()
        && !run.stop.is_cancelled()
    {
        return Err(Error::other("notification already started"));
    }
    let mut notify = db.listen(stop.clone(), CHANNEL).await?;
    let (jobs_tx, mut jobs_rx) = mpsc::channel::<Job>(JOB_BUFFER);
    let hub = Arc::new(Hub {
        clients: Mutex::new(HashMap::new()),
    });
    let runner = Arc::new(Runner {
        db,
        hub: hub.clone(),
        jobs: jobs_tx,
        stop: stop.clone(),
        closed: Mutex::new(false),
    });
    {
        let runner = runner.clone();
        let stop = stop.clone();
        tokio::spawn(async move {
            loop {
                tokio::select! {
                    _ = stop.cancelled() => {
                        *runner.closed.lock().expect("closed") = true;
                        let deadline = tokio::time::Instant::now() + DRAIN_TIMEOUT;
                        while let Ok(job) = jobs_rx.try_recv() {
                            if tokio::time::Instant::now() >= deadline {
                                break;
                            }
                            exec(runner.db, job).await;
                        }
                        return;
                    }
                    job = jobs_rx.recv() => {
                        match job {
                            Some(job) => exec(runner.db, job).await,
                            None => return,
                        }
                    }
                }
            }
        });
    }
    {
        let runner = runner.clone();
        tokio::spawn(async move {
            while let Some(n) = notify.recv().await {
                dispatch(&runner, &n.payload).await;
            }
            runner.hub.close_all();
            tracing::info!("notification runner stopped");
        });
    }
    *slot.lock().expect("notification") = Some(runner);
    tracing::info!("notification runner started");
    Ok(())
}

pub fn write(input: Notification, object_type: &str, object_id: Uuid) {
    let qualified = match resolve_object_type(object_type) {
        Ok(q) => q,
        Err(e) => {
            tracing::error!("notification Write: {e}");
            return;
        }
    };
    enqueue(Job {
        input,
        object_type: Some(qualified.0),
        object_id: Some(object_id),
        user_ids: Vec::new(),
    });
}

pub fn write_user(input: Notification, user_ids: &[Uuid]) {
    if user_ids.is_empty() {
        return;
    }
    let mut job = Job {
        input: input.clone(),
        object_type: None,
        object_id: None,
        user_ids: user_ids.to_vec(),
    };
    if !input.object_type.is_empty()
        && let Some(object_id) = input.object_id
    {
        match resolve_object_type(&input.object_type) {
            Ok(q) => {
                job.object_type = Some(q.0);
                job.object_id = Some(object_id);
            }
            Err(e) => {
                tracing::error!("notification WriteUser: {e}");
                return;
            }
        }
    }
    enqueue(job);
}

fn enqueue(job: Job) {
    let Some(run) = current() else {
        tracing::error!(
            "notification {:?} dropped: notification not started: call sam.Init",
            job.input.title
        );
        return;
    };
    if *run.closed.lock().expect("closed") {
        tracing::error!(
            "notification {:?} dropped: the runner has ended",
            job.input.title
        );
        return;
    }
    if run.jobs.try_send(job).is_err() {
        tracing::error!("notification dropped: job queue full");
    }
}

async fn exec(db: &Db, job: Job) {
    let input = job.input;
    let style = if input.style.is_null() {
        Value::Object(serde_json::Map::new())
    } else {
        input.style.clone()
    };
    let ctx = Ctx::new();
    if !job.user_ids.is_empty() {
        let result = db
            .query_scalar::<i32>(
                &ctx,
                "SELECT claimius.notify_user($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11::text::interval, $12::uuid[])",
                &[&input.app_id, &input.title, &input.description, &style, &input.r#type, &input.link, &job.object_type, &job.object_id, &input.created_by, &input.owner_id, &input.repeats_at, &job.user_ids],
            )
            .await;
        if let Err(e) = result {
            tracing::error!("notification WriteUser exec: {e}");
        }
        return;
    }
    let result = db
        .query_scalar::<i32>(
            &ctx,
            "SELECT claimius.notify_object($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10, $11::text::interval)",
            &[&input.app_id, &input.title, &input.description, &style, &input.r#type, &input.link, &job.object_type, &job.object_id, &input.created_by, &input.owner_id, &input.repeats_at],
        )
        .await;
    if let Err(e) = result {
        tracing::error!("notification Write exec: {e}");
    }
}

async fn dispatch(runner: &Runner, payload: &str) {
    #[derive(serde::Deserialize)]
    struct Message {
        user_id: Uuid,
        id: Uuid,
    }
    let message: Message = match serde_json::from_str(payload) {
        Ok(m) => m,
        Err(e) => {
            tracing::error!("notification dispatch decode: {e}");
            return;
        }
    };
    if !runner.hub.has(message.user_id) {
        return;
    }
    let row = match runner
        .db
        .query_model_opt::<crate::shared::Notification>(
            &Ctx::new(),
            "SELECT * FROM claimius.notification WHERE id = $1 AND sa_deleted_at IS NULL LIMIT 1",
            &[&message.id],
        )
        .await
    {
        Ok(Some(n)) => n,
        Ok(None) => return,
        Err(e) => {
            tracing::error!("notification dispatch load: {e}");
            return;
        }
    };
    match serde_json::to_vec(&row) {
        Ok(data) => runner.hub.push(message.user_id, data),
        Err(e) => tracing::error!("notification dispatch marshal: {e}"),
    }
}

pub fn stream(user_id: Uuid) -> Response {
    let Some(run) = current() else {
        return (
            http::StatusCode::INTERNAL_SERVER_ERROR,
            "notification not started: call sam.Init",
        )
            .into_response();
    };
    let mut rx = run.hub.register(user_id);
    let stop = run.stop.clone();
    let serve = serve_token().clone();
    let events = async_stream(move |tx| async move {
        loop {
            tokio::select! {
                _ = stop.cancelled() => return,
                _ = serve.cancelled() => return,
                message = rx.recv() => {
                    match message {
                        None => return,
                        Some(data) => {
                            let text = String::from_utf8_lossy(&data).to_string();
                            if tx.send(Ok::<Event, Infallible>(Event::default().data(text))).await.is_err() {
                                return;
                            }
                        }
                    }
                }
            }
        }
    });
    let mut response = Sse::new(events)
        .keep_alive(KeepAlive::new().interval(HEARTBEAT).text("heartbeat"))
        .into_response();
    response
        .headers_mut()
        .insert("X-Accel-Buffering", http::HeaderValue::from_static("no"));
    response
}

fn async_stream<F, Fut>(
    f: F,
) -> tokio_stream::wrappers::ReceiverStream<std::result::Result<Event, Infallible>>
where
    F: FnOnce(mpsc::Sender<std::result::Result<Event, Infallible>>) -> Fut,
    Fut: std::future::Future<Output = ()> + Send + 'static,
{
    let (tx, rx) = mpsc::channel(CLIENT_BUFFER);
    tokio::spawn(f(tx));
    tokio_stream::wrappers::ReceiverStream::new(rx)
}

pub mod tokio_stream {
    pub mod wrappers {
        use std::pin::Pin;
        use std::task::{Context, Poll};

        use futures::Stream;
        use tokio::sync::mpsc::Receiver;

        pub struct ReceiverStream<T> {
            inner: Receiver<T>,
        }

        impl<T> ReceiverStream<T> {
            pub fn new(inner: Receiver<T>) -> Self {
                ReceiverStream { inner }
            }
        }

        impl<T> Stream for ReceiverStream<T> {
            type Item = T;

            fn poll_next(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<T>> {
                self.inner.poll_recv(cx)
            }
        }
    }
}
