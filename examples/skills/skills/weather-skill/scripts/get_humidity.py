import argparse


def get_humidity(location: str) -> str:
    """Fetch live humidity for a given location. (Simulated)"""
    print(f"Fetching live humidity for {location}...")
    return "45% (Simulated)"


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--location", type=str, default="Mountain View")
    args = parser.parse_args()
    print(get_humidity(args.location))
