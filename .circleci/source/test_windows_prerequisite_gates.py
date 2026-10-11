"""The historical shell comparison cannot replace production prerequisites."""
import pathlib
import re
import unittest


def validate(config, script):
    assert "powershell-launch-diagnostic:\n    type: boolean\n    default: false" in config
    job = re.search(r"(?ms)^  windows-preview-process:\n(.*?)(?=^  \S)", config).group(1)
    assert re.search(r"(?m)^          command: \.\\\.circleci\\test-source-preview-windows-process\.ps1$", job)
    assert "LiteralDiagnosticOnly" not in job and "condition:" not in job
    assert job.count("test-windows-preview-owner.ps1") == 1
    assert job.count("test-windows-preview-wiring.ps1") == 1
    assert "requires: [windows-preview-process]" in config
    assert "agent-capability-build:\n    when:\n      not: << pipeline.parameters.powershell-launch-diagnostic >>\n    jobs:" in config
    assert "powershell-launch-diagnostic:\n    when: << pipeline.parameters.powershell-launch-diagnostic >>\n    jobs:\n      - windows-powershell-launch-diagnostic" in config
    assert config.count("-LiteralDiagnosticOnly") == 1
    assert script.startswith("param([switch]$LiteralDiagnosticOnly)\n")
    before, after = script.split("    # End optional historical comparison.\n")
    assert "if ($LiteralDiagnosticOnly) {" in before and "        return\n    }\n" in before
    assert before.count("TestWindowsPowerShellDetachedLiteral$") == 1
    for required in ("TestWindowsSignatureVerifierSystemFile$", "TestWindowsSignatureVerifierExactGDIPlus$", "-count=500", "-count=100", "-count=20", "& $go vet ./..."):
        assert required not in before and required in after
    assert "LiteralDiagnosticOnly" not in after


class ProductionPrerequisiteTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        root = pathlib.Path(__file__).resolve().parents[1]
        cls.config = (root / "config.yml").read_text()
        cls.script = (root / "test-source-preview-windows-process.ps1").read_text()

    def test_default_and_optional_are_separate(self):
        validate(self.config, self.script)

    def test_missing_signature_gate_rejected(self):
        for name in ("TestWindowsSignatureVerifierSystemFile$", "TestWindowsSignatureVerifierExactGDIPlus$"):
            with self.assertRaises(AssertionError):
                validate(self.config, self.script.replace(name, "Omitted$"))

    def test_optional_mode_cannot_replace_default(self):
        with self.assertRaises(AssertionError):
            validate(self.config.replace("command: .\\.circleci\\test-source-preview-windows-process.ps1\n", "command: .\\.circleci\\test-source-preview-windows-process.ps1 -LiteralDiagnosticOnly\n"), self.script)

    def test_default_cannot_skip_owner_or_wiring(self):
        for name in ("test-windows-preview-owner.ps1", "test-windows-preview-wiring.ps1"):
            with self.assertRaises(AssertionError):
                validate(self.config.replace(name, "omitted.ps1"), self.script)

    def test_diagnostic_cannot_be_default(self):
        with self.assertRaises(AssertionError):
            validate(self.config.replace("default: false", "default: true"), self.script)


if __name__ == "__main__":
    unittest.main()
