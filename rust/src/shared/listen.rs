use std::time::Duration;

use futures::StreamExt;
use tokio::sync::mpsc;
use tokio_postgres::{AsyncMessage, NoTls};

use super::db::Db;
use super::error::{Error, Result};

const LISTEN_BUFFER: usize = 1024;

#[derive(Clone, Debug)]
pub struct Notification {
    pub channel: String,
    pub payload: String,
}

impl Db {
    pub async fn listen(
        &self,
        stop: tokio_util::sync::CancellationToken,
        channel: &str,
    ) -> Result<mpsc::Receiver<Notification>> {
        let (tx, rx) = mpsc::channel(LISTEN_BUFFER);
        let conn_str = self.conn_str.clone();
        let channel = channel.to_string();
        let (client, mut connection) = tokio_postgres::connect(&conn_str, NoTls)
            .await
            .map_err(|e| Error::other(format!("listener: {e}")))?;
        let (notify_tx, mut notify_rx) = mpsc::unbounded_channel::<AsyncMessage>();
        let mut stream = futures::stream::poll_fn(move |cx| connection.poll_message(cx));
        tokio::spawn(async move {
            while let Some(message) = stream.next().await {
                match message {
                    Ok(m) => {
                        if notify_tx.send(m).is_err() {
                            return;
                        }
                    }
                    Err(e) => {
                        tracing::error!("listener: {e}");
                        return;
                    }
                }
            }
        });
        client
            .batch_execute(&format!("LISTEN \"{channel}\""))
            .await
            .map_err(|e| Error::other(format!("listener: listen {channel}: {e}")))?;
        tokio::spawn(async move {
            let mut ping = tokio::time::interval(Duration::from_secs(90));
            ping.tick().await;
            loop {
                tokio::select! {
                    _ = stop.cancelled() => {
                        let _ = client.batch_execute(&format!("UNLISTEN \"{channel}\"")).await;
                        return;
                    }
                    _ = ping.tick() => {
                        if let Err(e) = client.simple_query("SELECT 1").await {
                            tracing::error!("listener ping: {e}");
                            return;
                        }
                    }
                    message = notify_rx.recv() => {
                        match message {
                            None => return,
                            Some(AsyncMessage::Notification(n)) => {
                                if n.channel() != channel {
                                    continue;
                                }
                                let item = Notification { channel: n.channel().to_string(), payload: n.payload().to_string() };
                                if tx.try_send(item).is_err() {
                                    tracing::error!("listener: {channel} subscriber full, dropped");
                                }
                            }
                            Some(_) => {}
                        }
                    }
                }
            }
        });
        Ok(rx)
    }
}
