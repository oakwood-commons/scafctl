coverage:
  status:
    project:
      default:
        target: 70%
        threshold: 1%
    patch:
      default:
        target: 70%
        threshold: 5%

comment:
  layout: "reach,diff,flags,tree"
  behavior: default
  require_changes: false
  require_base: false
  require_head: true
