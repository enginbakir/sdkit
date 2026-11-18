# Template of the formula that goreleaser generates into dist/homebrew/Formula/ (.goreleaser.yaml, `brews`).
# The release pipeline fills the __PLACEHOLDERS__ (sha256 values from checksums.txt); the version is bumped with the release (P5-05).
class Sdkit < Formula
  desc "Spec-driven development workflow and guards for AI coding agents (Claude Code, Codex, OpenCode, Cursor, Gemini)"
  homepage "https://github.com/wecii/sdkit"
  version "2.3.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_arm64.tar.gz"
      sha256 "3dec40578e2dcd9f669d63d1296b1c5fe512b52639500199cc978af0c7e34b2f"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_amd64.tar.gz"
      sha256 "9add8018a27e859cd7edcd6a090a0ca7ae89c05b1a4edf318647843d7df6d76e"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_arm64.tar.gz"
      sha256 "d846c496d0555d3382926ce9c4ababbfb9efc3ef583fbf4271de3d56b4171cd2"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_amd64.tar.gz"
      sha256 "49546a11d857d32e556a09b7ebca4ce9678eb8577b84f0249d1f5aa56d86d432"
    end
  end

  def install
    bin.install "sdkit"
  end

  def caveats
    <<~EOS
      Set a repo up (once per repo, by one person):
        cd your-repo && sdkit init
      Check a repo any time:  sdkit doctor
      Upgrade a repo after "brew upgrade":  sdkit upgrade
    EOS
  end

  test do
    assert_match "sdkit", shell_output("#{bin}/sdkit version")
    refute_empty shell_output("#{bin}/sdkit where").strip
  end
end
