use clap::Parser;

fn main() {
    let args = go_check_spam::Args::parse();
    if let Err(err) = go_check_spam::run(args) {
        eprintln!("Error getting spam counts: {}", err);
        std::process::exit(1);
    }
}
