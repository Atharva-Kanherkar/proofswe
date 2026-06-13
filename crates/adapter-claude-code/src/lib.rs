use proofswe_core::{Harness, SourceAdapter};

#[derive(Debug, Default)]
pub struct ClaudeCodeAdapter;

impl SourceAdapter for ClaudeCodeAdapter {
    fn harness(&self) -> Harness {
        Harness::ClaudeCode
    }
}

#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum ClaudeCodeAdapterError {
    #[error("claude-code capture logic is not implemented yet")]
    NotImplemented,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn adapter_reports_claude_code_harness() {
        assert_eq!(ClaudeCodeAdapter.harness(), Harness::ClaudeCode);
    }
}
