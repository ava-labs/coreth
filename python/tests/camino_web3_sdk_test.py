import unittest

from web3 import Web3

# Establish connection to the blockchain using an HTTP provider
w3 = Web3(Web3.HTTPProvider('https://kopernikus.camino.network:443/ext/bc/C/rpc'))

# Hash of a sample transaction to be used in the tests
tx_hash = "0x1bc00abfed5137c19fbf98ac128ff4039702d8213b223c1df400881aaebc8200"

# flexdeposittest1 kopernikus account
test_sender_pk = "d4731145f6ec3d9b5fae16be751b40f37d6472f2f421e496231bec3376d25a08"
test_sender_address = "0x5abd2c1a77d0647d83b80d413608a5d5a3d466af"

checksum_sender_address = w3.to_checksum_address(test_sender_address)

class Web3TestCase(unittest.TestCase):

  # Test to check if we can retrieve a transaction from the blockchain using its hash
  def test_get_transaction(self):
    tx = w3.eth.get_transaction(tx_hash)
    self.assertIsNotNone(tx)  # Assert that the returned transaction is not None

  # Test to check if we can retrieve a block from the blockchain using a transaction's blockHash
  def test_get_block(self):
    tx = w3.eth.get_transaction(tx_hash)
    block = w3.eth.get_block(tx.blockHash)
    self.assertIsNotNone(block)  # Assert that the returned block is not None

  # Test to check if we can retrieve the receipt of a transaction from the blockchain using its hash
  def test_get_transaction_receipt(self):
    receipt = w3.eth.get_transaction_receipt(tx_hash)
    self.assertIsNotNone(receipt)  # Assert that the returned receipt is not None

  # Test to check if we can retrieve the latest block number from the blockchain
  def test_get_block_number(self):
    block_number = w3.eth.get_block_number()
    self.assertIsNotNone(block_number)  # Assert that the returned block number is not None

  # Test to check if we can retrieve the balance of an account
  def test_get_balance(self):
    tx = w3.eth.get_transaction(tx_hash)
    account = tx["to"]
    balance = w3.eth.get_balance(account)
    self.assertIsNotNone(balance)  # Assert that the returned balance is not None


  # Test to check if we can successfully send a transaction to the blockchain
  def test_send_raw_transaction(self):
    # Generate a new recipient account and get its address
    recipient_account = w3.eth.account.create()
    recipient_address = recipient_account.address

    # Create a dummy transaction from the sender to the recipient
    tx = {
      'from': checksum_sender_address,
      'to': recipient_address,
      'value': w3.eth.w3.to_wei(0.1, 'ether'),
      'gas': 21000,
      'gasPrice': w3.eth.w3.to_wei('200', 'gwei'),
      'nonce': w3.eth.get_transaction_count(checksum_sender_address),
      'chainId': 502
    }

    private_key = test_sender_pk.lower()
    # Sign the transaction with the sender's private key
    signed_tx = w3.eth.account.sign_transaction(tx, private_key)

    # Serialize the signed transaction
    raw_tx = signed_tx.rawTransaction

    # Send the signed transaction to the network
    sent_raw_tx = w3.eth.send_raw_transaction(signed_tx.rawTransaction)

    # recover the transaction sender from the signed transaction data
    recovered_sender = w3.eth.account.recover_transaction(raw_tx)

    # Check that the transaction was successfully sent
    self.assertIsNotNone(sent_raw_tx)

    # assert that the recovered sender is the same as the original sender
    assert recovered_sender == checksum_sender_address
