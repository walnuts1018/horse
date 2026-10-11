env "kubernetes" {
  url = getenv("DATABASE_URL")

  migration {
    dir = "file://migrations"
  }
}
