use std::collections::BTreeSet;

use super::update_purchase_item::UpdatePurchaseItemCommand;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SavePurchaseItemEditsCommand {
    pub purchase_id: String,
    pub items: Vec<UpdatePurchaseItemCommand>,
    pub purchase_patch_present: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SavePurchaseItemEditsDecision {
    pub item_ids: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SavePurchaseItemEditsError {
    MissingChanges,
    TooManyItems,
    DuplicateItem,
}

pub fn decide_save_purchase_item_edits(
    command: SavePurchaseItemEditsCommand,
) -> Result<SavePurchaseItemEditsDecision, SavePurchaseItemEditsError> {
    if command.items.is_empty() && !command.purchase_patch_present {
        return Err(SavePurchaseItemEditsError::MissingChanges);
    }
    if command.items.len() > 200 {
        return Err(SavePurchaseItemEditsError::TooManyItems);
    }
    let mut item_ids = BTreeSet::new();
    for item in &command.items {
        if !item_ids.insert(item.item_id.as_str()) {
            return Err(SavePurchaseItemEditsError::DuplicateItem);
        }
    }
    Ok(SavePurchaseItemEditsDecision {
        item_ids: command.items.into_iter().map(|item| item.item_id).collect(),
    })
}
