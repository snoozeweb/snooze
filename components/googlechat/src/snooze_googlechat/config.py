import os
from pathlib import Path

import yaml
from pydantic import BaseModel, Field
from google.auth.jwt import Credentials


class Config(BaseModel):
    """Configuration of snooze GoogleChat"""

    project_id: str
    service_account_path: str
    subscription_name: str
    snooze_url: str
    date_format: str = Field(default="%a, %b %d, %Y at %I:%M %p")
    bot_name: str = Field(default="Bot")
    use_card: bool = Field(default=False)
    debug: bool = Field(default=False)

    message_limit: int = Field(default=10)

    listening_address: str = Field(default="0.0.0.0")
    listening_port: int = Field(default=5201)


def load_config() -> Config:
    """Load the configuration file"""
    config_dir = os.environ.get("SNOOZE_GOOGLE_CHATBOT_PATH", "/etc/snooze")
    config_file = Path(config_dir) / "googlechat.yaml"
    try:
        data = yaml.safe_load(config_file.read_text())
        return Config(**data)
    except FileNotFoundError as error:
        raise FileNotFoundError(error, f"file {config_file} not found")


def get_credentials(config: Config) -> Credentials:
    return Credentials.from_service_account_file(config.service_account_path)
