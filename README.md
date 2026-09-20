# Terrablade

Terrablade is a deterministic, idempotent formatter for Terraform and OpenTofu
native HCL.

## Example

Input:

```terraform
resource aws_instance web {
ami="${var.ami}"
instance_type=var.instance_type


tags={
    Name = "example"
    Description = "example"
}
}
```

Output:

```terraform
resource "aws_instance" "web" {
  ami           = var.ami
  instance_type = var.instance_type

  tags = {
    Name        = "example",
    Description = "example",
  }
}
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
