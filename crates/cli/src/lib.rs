use lexopt::prelude::*;

pub const VERSION: &str = env!("CARGO_PKG_VERSION");

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Command {
    Help,
    Version,
}

pub fn parse_command<I, S>(args: I) -> Result<Command, lexopt::Error>
where
    I: IntoIterator<Item = S>,
    S: Into<std::ffi::OsString>,
{
    let mut parser = lexopt::Parser::from_args(args);
    let Some(arg) = parser.next()? else {
        return Ok(Command::Help);
    };

    let command = match arg {
        Value(value) if value == "help" => Command::Help,
        Value(value) if value == "version" => Command::Version,
        Value(value) => {
            return Err(format!("unknown command: {}", value.to_string_lossy()).into());
        }
        Long("help") | Short('h') => Command::Help,
        Long("version") | Short('V') => Command::Version,
        unexpected => return Err(unexpected.unexpected()),
    };

    if let Some(extra) = parser.next()? {
        return Err(extra.unexpected());
    }

    Ok(command)
}

pub fn usage() -> &'static str {
    "Usage: proofswe <command>\n\nCommands:\n  proofswe help      Print this help text\n  proofswe version   Print the proofswe version\n"
}

pub fn version() -> &'static str {
    VERSION
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_command_defaults_to_help() {
        assert_eq!(
            parse_command(std::iter::empty::<&str>()).unwrap(),
            Command::Help
        );
    }

    #[test]
    fn parse_command_accepts_help() {
        assert_eq!(parse_command(["help"]).unwrap(), Command::Help);
        assert_eq!(parse_command(["--help"]).unwrap(), Command::Help);
    }

    #[test]
    fn parse_command_accepts_version() {
        assert_eq!(parse_command(["version"]).unwrap(), Command::Version);
        assert_eq!(parse_command(["--version"]).unwrap(), Command::Version);
    }

    #[test]
    fn parse_command_rejects_unknown_subcommand() {
        let err = parse_command(["nope"]).unwrap_err().to_string();
        assert!(err.contains("unknown command: nope"));
    }

    #[test]
    fn parse_command_rejects_extra_arguments() {
        assert!(parse_command(["help", "extra"]).is_err());
        assert!(parse_command(["version", "extra"]).is_err());
    }
}
