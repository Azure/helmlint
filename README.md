# `helmlint`

Go library for testing Helm charts.


## Why?

A way to parse Helm charts into a complete AST doesn't currently exist.
Good parsers exist for Go templates and YAML, but not Go templates + YAML.
For large charts it's important to lint every branch of the chart's control flow.
But without an AST it's impossible to know when a branch isn't reached.

`helmlint` provides a workaround: injecting comments under every `if` statement and checking for them in the rendered chart output.
Missing comments will fail the test unless ignored.

The library uses `conftest` to handle the actual linting (bring your own policies).

## Usage

See the examples directory to get started and the Godocs for complete documentation of more obscure options.


## Contributing

This project welcomes contributions and suggestions.  Most contributions require you to agree to a
Contributor License Agreement (CLA) declaring that you have the right to, and actually do, grant us
the rights to use your contribution. For details, visit https://cla.opensource.microsoft.com.

When you submit a pull request, a CLA bot will automatically determine whether you need to provide
a CLA and decorate the PR appropriately (e.g., status check, comment). Simply follow the instructions
provided by the bot. You will only need to do this once across all repos using our CLA.

This project has adopted the [Microsoft Open Source Code of Conduct](https://opensource.microsoft.com/codeofconduct/).
For more information see the [Code of Conduct FAQ](https://opensource.microsoft.com/codeofconduct/faq/) or
contact [opencode@microsoft.com](mailto:opencode@microsoft.com) with any additional questions or comments.

## Trademarks

This project may contain trademarks or logos for projects, products, or services. Authorized use of Microsoft 
trademarks or logos is subject to and must follow 
[Microsoft's Trademark & Brand Guidelines](https://www.microsoft.com/en-us/legal/intellectualproperty/trademarks/usage/general).
Use of Microsoft trademarks or logos in modified versions of this project must not cause confusion or imply Microsoft sponsorship.
Any use of third-party trademarks or logos are subject to those third-party's policies.
