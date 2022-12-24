# Release Notes
## Coreth release notes: [RELEASES](https://github.com/ava-labs/coreth/blob/master/RELEASES.md)

## [v0.2.0](https://github.com/chain4travel/caminoethvm/releases/tag/v0.2.0)
- Based on coreth [v0.11.1](https://github.com/ava-labs/coreth/releases/tag/v0.11.1)
- Implemented Sunrise Release tags
- Fixed BaseFee 50nCAM
- Depends on new CaminoGo v0.3.0 SDK

## [v0.4.0](https://github.com/chain4travel/caminoethvm/releases/tag/v0.4.0)
- Based on coreth [v0.11.4](https://github.com/ava-labs/coreth/releases/tag/v0.11.4)
- Depends on new CaminoGo v0.4.0 SDK
- Use git submodule for caminogo dependency
- Pre-deployed smart contracts AdminController and IncentivePool (including access control)
- Only KYC can deploy smart contracts
- Add kopernikus predefined network
- Re-define networkIDs (mainnet 500, columbus: 501 and kopernikus 502)
- Various CI workflow adjustments