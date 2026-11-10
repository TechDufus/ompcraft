package update

// ReleasePublicKey is the base64 (std) ed25519 public key (32 bytes) that
// signs every release's checksums.txt (checksums.txt.sig). While it is empty
// the updater refuses to install anything: "this build has no release key;
// updates are disabled".
const ReleasePublicKey = "LxgB7BgmB5U1IdNG9dKIandjg2E4DC/A5sU0Efah234="
