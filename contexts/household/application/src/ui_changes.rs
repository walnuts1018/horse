use horse_household_change::UiChangeDescriptor;

pub fn default_allocation_updated(fields: &[String]) -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "allocation.default.updated",
        "household",
        fields.iter().map(String::as_str),
    )
}

pub fn membership_changed(
    change: household_domain::membership::MembershipChange,
) -> UiChangeDescriptor {
    use household_domain::membership::MembershipChange;
    match change {
        MembershipChange::Leave => {
            UiChangeDescriptor::new("household.member.left", "member", ["members"])
        }
        MembershipChange::Remove => {
            UiChangeDescriptor::new("household.member.removed", "member", ["members"])
        }
        MembershipChange::TransferOwner { .. } => UiChangeDescriptor::new(
            "household.owner.transferred",
            "member",
            ["members", "owner"],
        ),
    }
}

pub fn mail_forwarding_issued(rotated: bool) -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        if rotated {
            "household.mail_forwarding.rotated"
        } else {
            "household.mail_forwarding.issued"
        },
        "mail_forwarding",
        ["mailForwardingAddress"],
    )
}

pub fn mail_forwarding_revoked() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "household.mail_forwarding.revoked",
        "mail_forwarding",
        ["mailForwardingAddress"],
    )
}
