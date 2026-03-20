use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use std::path::PathBuf;
use tee::{TeeRuntime, default_state_path, ensure_quote_matches_sum};

#[derive(Debug, Parser)]
#[command(name = "tee")]
#[command(about = "Minimal ELF-based TEE simulator for a single adder guest")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Debug, Subcommand)]
enum Commands {
    Register {
        #[arg(long, default_value_os_t = default_state_path())]
        state: PathBuf,
    },
    Run {
        #[arg(long, default_value_os_t = default_state_path())]
        state: PathBuf,
        #[arg(long)]
        a: i64,
        #[arg(long)]
        b: i64,
        #[arg(long)]
        nonce: String,
        #[arg(long)]
        quote_out: Option<PathBuf>,
    },
    Verify {
        #[arg(long, default_value_os_t = default_state_path())]
        state: PathBuf,
        #[arg(long)]
        quote: PathBuf,
        #[arg(long)]
        nonce: String,
        #[arg(long)]
        guest_name: Option<String>,
    },
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    let runtime = TeeRuntime::new();
    match cli.command {
        Commands::Register { state } => {
            let registration = runtime.register_guest(&state)?;
            println!("{}", serde_json::to_string_pretty(&registration)?);
        }
        Commands::Run {
            state,
            a,
            b,
            nonce,
            quote_out,
        } => {
            let run = runtime.run_guest(&state, a, b, nonce)?;
            ensure_quote_matches_sum(&run.quote, run.sum)?;

            if let Some(path) = quote_out.as_ref() {
                runtime
                    .write_quote(path, &run.quote)
                    .with_context(|| format!("failed to persist quote to {}", path.display()))?;
            }

            println!("sum={}", run.sum);
            println!("measurement={}", run.measurement);
            println!("{}", serde_json::to_string_pretty(&run.quote)?);
        }
        Commands::Verify {
            state,
            quote,
            nonce,
            guest_name,
        } => {
            let registration = runtime.load_registration(&state)?;
            let quote = runtime.load_quote(&quote)?;
            let outcome = runtime.verify_quote(
                &quote,
                &registration.measurement,
                &nonce,
                guest_name.as_deref(),
            )?;
            println!("{}", serde_json::to_string_pretty(&outcome)?);
        }
    }

    Ok(())
}
