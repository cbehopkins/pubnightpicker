from datetime import date, datetime
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, field_validator

from firebase_sub.my_types import VenueType


class PollPayload(BaseModel):
    model_config = ConfigDict(extra="ignore")

    selected: str
    date: str
    completed: bool = False
    restaurant: str | None = None
    restaurant_time: str | None = None

    @field_validator("date", mode="before")
    @classmethod
    def normalize_date(cls, value: Any) -> str:
        if isinstance(value, datetime):
            return value.isoformat()
        if isinstance(value, date):
            return value.isoformat()
        if value is None:
            raise ValueError("poll date is required")
        return str(value)


class VenuePayload(BaseModel):
    model_config = ConfigDict(extra="ignore")

    name: str
    venue_type: VenueType = Field(default=VenueType.PUB, alias="venueType")
    web_site: str | None = None
    address: str | None = None
    map: str | None = None

    @field_validator("venue_type", mode="before")
    @classmethod
    def normalize_venue_type(cls, value: Any) -> str:
        if value is None or value == "":
            return VenueType.PUB.value
        return str(value).lower()
