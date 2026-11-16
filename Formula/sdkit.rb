# Template of the formula that goreleaser generates into dist/homebrew/Formula/ (.goreleaser.yaml, `brews`).
# The release pipeline fills the __PLACEHOLDERS__ (sha256 values from checksums.txt); the version is bumped with the release (P5-05).
class Sdkit < Formula
  desc "Spec-driven development workflow and guards for AI coding agents (Claude Code, Codex, OpenCode, Cursor, Gemini)"
  homepage "https://github.com/wecii/sdkit"
  version "2.1.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_arm64.tar.gz"
      sha256 "04de0eb9a5243b1bec71df634be8ba8e90e58fbaaa1f6ffe3f76ef1b62d29854"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_amd64.tar.gz"
      sha256 "aa63ef917bc355d5a20c0fb4f6f9aa0f330aedd8233a0b3fb799957afb8ae348"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_arm64.tar.gz"
      sha256 "999463a3e152f2df3417db3be9a07d4fad7cad8f60de6f37d9c98b231502f101"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_amd64.tar.gz"
      sha256 "960240e663798f31e7465b13ea0e1ad44bb02dd5734cd6266484af00d911d274"
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
