# Template of the formula that goreleaser generates into dist/homebrew/Formula/ (.goreleaser.yaml, `brews`).
# The release pipeline fills the __PLACEHOLDERS__ (sha256 values from checksums.txt); the version is bumped with the release (P5-05).
class Sdkit < Formula
  desc "Spec-driven development workflow and guards for AI coding agents (Claude Code, Codex, OpenCode, Cursor, Gemini)"
  homepage "https://github.com/wecii/sdkit"
  version "2.3.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_arm64.tar.gz"
      sha256 "b4c3faf238cbac6f3cf68abb93b65ef417eb0401ec06d32e829ac7549c749831"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_amd64.tar.gz"
      sha256 "f6e78515d59a04c4568ad4d28e2eee47f68b7ea0e7a669ef440c212563059b8e"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_arm64.tar.gz"
      sha256 "ee690940618537c65d533eb4cef46c52cb84cbb7320539a60c95140209e69844"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_amd64.tar.gz"
      sha256 "8f14e190882fd9b106c072aa2980d855695fb352c20de7ea6107f7282c0dd0b9"
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
