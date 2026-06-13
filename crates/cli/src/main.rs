use anyhow::Result;
use cli::{Command, parse_command, usage, version};

fn main() -> Result<()> {
    match parse_command(std::env::args_os().skip(1)) {
        Ok(Command::Help) => {
            print!("{}", usage());
            Ok(())
        }
        Ok(Command::Version) => {
            println!("{}", version());
            Ok(())
        }
        Err(err) => {
            eprintln!("error: {err}\n\n{}", usage());
            std::process::exit(2);
        }
    }
}
