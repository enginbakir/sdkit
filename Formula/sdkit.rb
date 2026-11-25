# Template of the formula that goreleaser generates into dist/homebrew/Formula/ (.goreleaser.yaml, `brews`).
# The release pipeline fills the __PLACEHOLDERS__ (sha256 values from checksums.txt); the version is bumped with the release (P5-05).
class Sdkit < Formula
  desc "Spec-driven development workflow and guards for AI coding agents (Claude Code, Codex, OpenCode, Cursor, Gemini)"
  homepage "https://github.com/wecii/sdkit"
  version "3.0.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_arm64.tar.gz"
      sha256 "5afcbb3b26c2194dc4f05a09369e9eddc3e2f478bf1944e43f1a97d12ab36bbd"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_amd64.tar.gz"
      sha256 "6af594082167488a7f4c42e96a7b84490ed74b2ee8559863787c468dab45fc09"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_arm64.tar.gz"
      sha256 "0f95f150c9981b9ca1e12f6ae33f33b239bad62db09b7cdc51f3a17547c40089"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_amd64.tar.gz"
      sha256 "a4f7d744fcc1e435a1096b499627079b87c862f4d2fb6e3e254b4592fb5d2c23"
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
