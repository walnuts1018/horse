mod scoring;

use std::{collections::HashSet, env, fs, process};

use scoring::{Dataset, score};

fn main() {
    if let Err(error) = run() {
        eprintln!("{error}");
        process::exit(1);
    }
}

fn run() -> Result<(), String> {
    let mut args = env::args().skip(1);
    let mut dataset_path = None;
    while let Some(arg) = args.next() {
        if arg == "--dataset" {
            if dataset_path.is_some() {
                return Err("--dataset may be specified only once".into());
            }
            dataset_path = args.next();
            if dataset_path.is_none() {
                return Err("--dataset requires a path".into());
            }
        } else {
            return Err(format!("unexpected argument: {arg}"));
        }
    }
    let path = dataset_path.ok_or_else(|| "--dataset is required".to_string())?;
    let bytes = fs::read(path).map_err(|error| format!("open evaluation dataset: {error}"))?;
    if bytes.len() > 32 << 20 {
        return Err("evaluation dataset exceeds 32 MiB".into());
    }
    let dataset: Dataset = serde_json::from_slice(&bytes)
        .map_err(|error| format!("decode evaluation dataset: {error}"))?;
    if dataset.version.is_empty() || dataset.cases.is_empty() {
        return Err("evaluation dataset requires a version and at least one case".into());
    }
    let mut ids = HashSet::with_capacity(dataset.cases.len());
    for case in &dataset.cases {
        if case.id.is_empty() {
            return Err("evaluation case ID is required".into());
        }
        if !ids.insert(&case.id) {
            return Err(format!("duplicate evaluation case ID {:?}", case.id));
        }
    }
    let output = serde_json::to_string(&score(dataset))
        .map_err(|error| format!("encode evaluation metrics: {error}"))?;
    println!("{output}");
    Ok(())
}
