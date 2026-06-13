use std::fmt;

#[non_exhaustive]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Harness {
    ClaudeCode,
    Codex,
}

impl fmt::Display for Harness {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::ClaudeCode => f.write_str("claude-code"),
            Self::Codex => f.write_str("codex"),
        }
    }
}

#[non_exhaustive]
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NormalizedEvent {
    Placeholder { harness: Harness },
}

#[derive(Debug, thiserror::Error)]
#[non_exhaustive]
pub enum ProofsweError {
    #[error("capture logic is not implemented yet")]
    CaptureNotImplemented,
}

pub trait SourceAdapter {
    fn harness(&self) -> Harness;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn harness_display_uses_stable_slug() {
        assert_eq!(Harness::ClaudeCode.to_string(), "claude-code");
        assert_eq!(Harness::Codex.to_string(), "codex");
    }
}
