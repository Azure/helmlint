package main
import rego.v1

import data.kubernetes

name := input.metadata.name

deny contains msg if {
    input.metadata.labels["example.com/value"]
    msg = sprintf("%s must not include the forbidden label", [name])
}