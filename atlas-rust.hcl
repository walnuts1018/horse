env "rust" {
  url = getenv("RUST_DATABASE_URL")

  migration {
    dir = "file://rust-migrations"
  }
}
