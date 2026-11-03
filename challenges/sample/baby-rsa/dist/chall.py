# Baby RSA: how output.txt was made.
from Crypto.Util.number import getPrime, bytes_to_long

FLAG = open("flag.txt", "rb").read().strip()

e = 3
while True:
    p, q = getPrime(1024), getPrime(1024)
    if (p - 1) % e and (q - 1) % e:
        break
n = p * q
m = bytes_to_long(FLAG)  # no padding: it's faster this way
c = pow(m, e, n)

print(f"n = {n}")
print(f"e = {e}")
print(f"c = {c}")
