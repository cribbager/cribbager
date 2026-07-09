"""cribml: the training side of the cribbager ML bot.

Models are trained here with PyTorch and exported (cribml.export) to the JSON
weights format that the Go inference package internal/nn loads. The two
implementations are held equal by internal/nn's parity test, whose fixtures
scripts/make_parity_fixture.py generates.
"""
