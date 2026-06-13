use proofswe_core::{Harness, SourceAdapter};

#[derive(Debug, Default)]
pub struct CodexAdapter;

impl SourceAdapter for CodexAdapter {
    fn harness(&self) -> Harness {
        Harness::Codex
    }
}

#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum CodexAdapterError {
    #[error("codex capture logic is not implemented yet")]
    NotImplemented,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn adapter_reports_codex_harness() {
        assert_eq!(CodexAdapter.harness(), Harness::Codex);
    }
}
